package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxServer builds a dashServer over a throwaway state dir holding one
// project with one ticket, and returns it plus that ticket's home.
func sandboxServer(t *testing.T) (*dashServer, string) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("BABYSIT_HOME", state)
	home := filepath.Join(state, "projects", "proj", "tickets", "bs-aaaa1111")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	idx := `{"id":"bs-aaaa1111","status":"planned","title":"a ticket"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "index.json"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	return &dashServer{stateDir: state, distDir: t.TempDir(), version: "test",
		origin: "http://127.0.0.1:1234"}, home
}

func post(t *testing.T, s *dashServer, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.mux().ServeHTTP(w, r)
	return w
}

func readIndex(t *testing.T, home string) map[string]interface{} {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("index.json is not valid JSON after the mutation: %v\n%s", err, b)
	}
	return m
}

// The served snapshot and data.js must be the same object — one composer, two
// transports. A served response missing the v2 keys means the SPA's single TS
// type does not in fact describe both modes.
func TestSnapshotServesComposeShape(t *testing.T) {
	s, _ := sandboxServer(t)
	w := httptest.NewRecorder()
	s.mux().ServeHTTP(w, httptest.NewRequest("GET", "/api/snapshot", nil))

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"meta", "projects", "decisions", "skillEvents", "builderProfile", "journalTail", "sessions"} {
		if _, ok := got[k]; !ok {
			t.Errorf("served snapshot is missing top-level %q", k)
		}
	}
	projects, _ := got["projects"].(map[string]interface{})
	if _, ok := projects["proj"]; !ok {
		t.Errorf("project not composed: %v", projects)
	}
}

// index.html loads ./data.js unconditionally. If the server let the stale file
// through, window.__BBS_DATA__ would be set and the SPA would render last
// week's snapshot instead of fetching live state.
func TestDataJSIsNeutralizedInServedMode(t *testing.T) {
	s, _ := sandboxServer(t)
	if err := os.WriteFile(filepath.Join(s.distDir, "data.js"),
		[]byte(`window.__BBS_DATA__ = {"stale":true};`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.mux().ServeHTTP(w, httptest.NewRequest("GET", "/data.js", nil))

	if strings.Contains(w.Body.String(), "__BBS_DATA__") {
		t.Errorf("stale data.js was served: %s", w.Body)
	}
}

func TestCreateTicketWritesRequirementAndIndex(t *testing.T) {
	s, _ := sandboxServer(t)
	w := post(t, s, "/api/tickets", `{"project":"proj","requirement":"Make the thing work\nmore detail"}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(resp["requirement"])
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "Make the thing work\nmore detail\n" {
		t.Errorf("requirement.md body: %q", body)
	}
	doc := readIndex(t, resp["home"])
	if doc["id"] != resp["ticket"] {
		t.Errorf("index id %v != %v", doc["id"], resp["ticket"])
	}
	if doc["title"] != "Make the thing work" {
		t.Errorf("title should default to the first requirement line, got %v", doc["title"])
	}
}

func TestCreateTicketNeedsARequirement(t *testing.T) {
	s, _ := sandboxServer(t)
	w := post(t, s, "/api/tickets", `{"project":"proj","requirement":"   "}`)
	if w.Code != 400 {
		t.Fatalf("blank requirement accepted: %d %s", w.Code, w.Body)
	}
}

func TestAssignWritesAssigneeAndReportsAnUnknownForeman(t *testing.T) {
	s, home := sandboxServer(t)
	w := post(t, s, "/api/tickets/proj/bs-aaaa1111/assign", `{"foreman":"fm-x"}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if got := readIndex(t, home)["assignee"]; got != "fm-x" {
		t.Errorf("assignee = %v", got)
	}
	// No such foreman record exists — the assignment still lands, and the
	// response says why nothing was poked rather than failing the write.
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["wake"] != "unknown-foreman" {
		t.Errorf("wake = %v, want unknown-foreman", resp["wake"])
	}
}

func TestAssignRejectsATraversingForemanID(t *testing.T) {
	s, home := sandboxServer(t)
	w := post(t, s, "/api/tickets/proj/bs-aaaa1111/assign", `{"foreman":"../../etc/x"}`)
	if w.Code != 400 {
		t.Fatalf("traversing foreman id accepted: %d %s", w.Code, w.Body)
	}
	if got, ok := readIndex(t, home)["assignee"]; ok && got != nil {
		t.Errorf("assignee was written anyway: %v", got)
	}
}

// The control axis is separate from the status ladder: pausing must leave
// status exactly where it was, or resume has nothing to return to.
func TestPauseNeverWritesStatus(t *testing.T) {
	s, home := sandboxServer(t)
	if w := post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"pause","note":"waiting on design"}`); w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	doc := readIndex(t, home)
	if doc["status"] != "planned" {
		t.Errorf("status was overwritten: %v", doc["status"])
	}
	ctl, _ := doc["control"].(map[string]interface{})
	if ctl["state"] != "paused" || ctl["prior_status"] != "planned" || ctl["note"] != "waiting on design" {
		t.Errorf("control = %v", ctl)
	}

	// …and it is reversible.
	if w := post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"resume"}`); w.Code != 200 {
		t.Fatalf("resume: %d %s", w.Code, w.Body)
	}
	after := readIndex(t, home)
	if after["control"] != nil {
		t.Errorf("control not cleared: %v", after["control"])
	}
	if after["status"] != "planned" {
		t.Errorf("status changed across pause/resume: %v", after["status"])
	}
}

// A cancel on a paused ticket is a conflict, not a silent overwrite — and the
// message has to name the verb that clears what is actually set.
func TestControlConflictIs409AndNamesTheUndoVerb(t *testing.T) {
	s, _ := sandboxServer(t)
	post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"pause"}`)

	w := post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"cancel"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "resume") {
		t.Errorf("conflict does not name the undo verb: %s", w.Body)
	}

	// Same for the clear side: restore must not un-pause.
	w = post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"restore"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("restore cleared a pause: %d %s", w.Code, w.Body)
	}
}

// Re-pausing an already-paused ticket is a no-op the UI treats as success, so
// the response still has to carry the rung the ticket is parked at — a blank
// status would blank the badge the dashboard just rendered.
func TestRepeatedPauseStillReportsTheStatus(t *testing.T) {
	s, _ := sandboxServer(t)
	post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"pause"}`)

	w := post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"pause"}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "planned" {
		t.Errorf("status = %q, want planned", resp["status"])
	}
}

// A ticket dir with no index.json is what a crashed create leaves behind.
// Treating it as a ticket would let the mutation finish materializing it.
func TestAnIndexlessTicketDirIsNotATicket(t *testing.T) {
	s, _ := sandboxServer(t)
	orphan := filepath.Join(s.stateDir, "projects", "proj", "tickets", "bs-orphan01")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if w := post(t, s, "/api/tickets/proj/bs-orphan01/control", `{"action":"pause"}`); w.Code != 400 {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(orphan, "index.json")); err == nil {
		t.Error("the refused mutation wrote an index.json anyway")
	}
}

func TestCreateTicketTakesAnExplicitTitle(t *testing.T) {
	s, _ := sandboxServer(t)
	w := post(t, s, "/api/tickets", `{"project":"proj","requirement":"line one\nline two","title":"A better title"}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got := readIndex(t, resp["home"])["title"]; got != "A better title" {
		t.Errorf("title = %v", got)
	}
}

func TestControlRejectsAnUnknownAction(t *testing.T) {
	s, _ := sandboxServer(t)
	if w := post(t, s, "/api/tickets/proj/bs-aaaa1111/control", `{"action":"delete"}`); w.Code != 400 {
		t.Fatalf("unknown action accepted: %d %s", w.Code, w.Body)
	}
}

// ticket.Store's own path sanitizer permits "/" and ".", so nothing below this
// layer stops a traversal — every id off the URL has to be checked here.
func TestPathsCannotEscapeTheStateDir(t *testing.T) {
	s, _ := sandboxServer(t)
	victim := filepath.Join(s.stateDir, "victim.json")
	if err := os.WriteFile(victim, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/tickets/proj/..%2f..%2fvictim/control",
		"/api/tickets/..%2f..%2fvictim/bs-aaaa1111/control",
		"/api/tickets/proj/.hidden/control",
	} {
		if w := post(t, s, path, `{"action":"pause"}`); w.Code != 400 && w.Code != 404 {
			t.Errorf("%s: status %d %s", path, w.Code, w.Body)
		}
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious\n" {
		t.Errorf("file outside the ticket tree was touched: %q", b)
	}
}

// A localhost control plane is reachable from any page the human has open, and
// the browser will send the POST even though it cannot read the reply.
func TestCrossOriginMutationIsRefused(t *testing.T) {
	s, home := sandboxServer(t)
	r := httptest.NewRequest("POST", "/api/tickets/proj/bs-aaaa1111/control",
		bytes.NewBufferString(`{"action":"cancel"}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.mux().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body)
	}
	if got := readIndex(t, home)["control"]; got != nil {
		t.Errorf("the mutation ran anyway: %v", got)
	}
}

func TestSameOriginMutationIsAllowed(t *testing.T) {
	s, _ := sandboxServer(t)
	r := httptest.NewRequest("POST", "/api/tickets/proj/bs-aaaa1111/control",
		bytes.NewBufferString(`{"action":"pause"}`))
	r.Header.Set("Origin", s.origin)
	w := httptest.NewRecorder()
	s.mux().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("same-origin POST refused: %d %s", w.Code, w.Body)
	}
}

// Pausing a ticket that does not exist must not create it — a typo'd id would
// otherwise leave a phantom ticket in the project.
func TestMutatingAnUnknownTicketDoesNotCreateIt(t *testing.T) {
	s, _ := sandboxServer(t)
	if w := post(t, s, "/api/tickets/proj/bs-nope0000/control", `{"action":"pause"}`); w.Code != 400 {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(s.stateDir, "projects", "proj", "tickets", "bs-nope0000")); err == nil {
		t.Error("a ticket directory was created by the failed mutation")
	}
}
