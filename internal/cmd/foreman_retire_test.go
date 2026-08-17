package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/foreman"
)

// Retiring has to do both halves: drop the record AND close the pane. A record
// dropped on its own leaves a workspace that still looks like a working foreman
// and can no longer be reached from the list it was just removed from.
func TestRetireClosesTheWorkspaceAndDropsTheRecord(t *testing.T) {
	log, _ := fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}
	rec, err := foreman.Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := retireForeman("fm-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreman.Load("fm-a"); err == nil {
		t.Error("retire left the record in place")
	}
	// The stub resolves the title to term_<n> before closing, so assert on
	// the close call rather than on the title.
	if calls := readCalls(t, log); !strings.Contains(calls, "terminal close") {
		t.Errorf("retire did not close the terminal %q:\n%s", rec.WorkspaceTitle, calls)
	}
	if strings.Contains(msg, "left open") {
		t.Errorf("workspace was closed, but the message claims otherwise: %q", msg)
	}
}

// The opt-out exists for a foreman whose pane the human still wants to read.
func TestRetireKeepsTheWorkspaceWhenAsked(t *testing.T) {
	log, _ := fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := retireForeman("fm-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := foreman.Load("fm-a"); err == nil {
		t.Error("retire left the record in place")
	}
	if calls := readCalls(t, log); strings.Contains(calls, "terminal close") {
		t.Errorf("keep-workspace still closed the pane:\n%s", calls)
	}
}

// Orca being gone is the common case for the foreman this feature exists to
// clear — the app was restarted hours ago. Retire must still drop the record,
// and must say what it could not close, or the human hunts for a pane that is
// not there.
func TestRetireDropsTheRecordWhenOrcaIsUnavailable(t *testing.T) {
	fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}
	// Break orca only after the spawn: PATH is a single temp dir, so removing
	// the stub is enough to make Preflight fail the way a stopped app does.
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		_ = os.Remove(filepath.Join(dir, "orca"))
	}

	msg, err := retireForeman("fm-a", false)
	if err != nil {
		t.Fatalf("orca being down must not block retiring a dead foreman: %v", err)
	}
	if _, err := foreman.Load("fm-a"); err == nil {
		t.Error("retire left the record in place")
	}
	if !strings.Contains(msg, "left open") {
		t.Errorf("message must name the workspace it could not close, got %q", msg)
	}
}

// The endpoint is the dashboard's only way to clear a record, so it has to reach
// the same code the CLI does — and reject an id that would escape the store.
func TestRetireEndpointClearsTheRecord(t *testing.T) {
	fakeOrcaFor(t)

	if _, err := spawnForeman("fm-a", t.TempDir(), "", ""); err != nil {
		t.Fatal(err)
	}

	srv := &dashServer{}
	mux := srv.mux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/foremen/fm-a/retire", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("retire endpoint: got %d, body %s", rec.Code, rec.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["retired"] != "fm-a" {
		t.Errorf("response does not name what it retired: %v", body)
	}
	if _, err := foreman.Load("fm-a"); err == nil {
		t.Error("endpoint returned OK but the record is still there")
	}

	// An illegal id must be refused by id validation, before anything turns it
	// into a store path — foreman.Load is where that happens for every caller.
	// Two traps make this assertion easy to write uselessly: a %2f-encoded
	// traversal never reaches the handler (net/http rejects it while routing),
	// and *any* unknown id already yields 400 further down, when the file is not
	// found. So the status code proves nothing on its own — assert on which check
	// spoke, by looking for the id rule in the message.
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("POST", "/api/foremen/..$X/retire", strings.NewReader(`{}`)))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("an illegal id was not refused: got %d, body %s", bad.Code, bad.Body)
	}
	if !strings.Contains(bad.Body.String(), "must be letters") {
		t.Errorf("id was rejected by a filesystem probe, not by ValidID: %s", bad.Body)
	}
}
