package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/cmux"
	"github.com/reallongnguyen/babysit/internal/dashboard"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// The served dashboard is the control plane: the same reads the file:// snapshot
// does, plus the mutations that used to be CLI-only. Two rules keep it honest:
//
//   - Reads go through dashboard.Compose, unmodified and unforked, so a served
//     GET /api/snapshot returns exactly the object data.js embeds. One schema,
//     one composer, two transports.
//   - Writes go through the same store functions the CLI verbs call, under the
//     same .index.lock. The server is a second writer to ticket state, not a
//     second implementation of it.
type dashServer struct {
	stateDir  string
	distDir   string
	version   string
	slug      string // deprecated --slug filter, honored here so both modes agree
	reconcile bool
	origin    string // "http://127.0.0.1:<port>", filled in once listening
}

// idRe is the shape of a slug or ticket id on the way to a file path. The CLI
// gets these from a branch name; the server gets them from a URL, where
// `../../` is one request away — and ticket.Store.safe() permits "/" and ".",
// so the store itself will not stop a traversal.
var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (s *dashServer) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/snapshot", s.handleSnapshot)
	m.HandleFunc("POST /api/tickets", s.handleCreateTicket)
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/assign", s.handleAssign)
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/control", s.handleControl)
	m.HandleFunc("POST /api/foremen", s.handleSpawnForeman)

	// index.html loads ./data.js unconditionally, and web/dist/data.js is
	// whatever the last snapshot run left there. Serving it here would boot the
	// SPA in snapshot mode against stale state; serving nothing leaves
	// window.__BBS_DATA__ undefined, which is exactly the signal the source
	// switch reads to take the fetch path.
	m.HandleFunc("GET /data.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintln(w, "// served mode: the SPA fetches /api/snapshot")
	})
	m.Handle("/", http.FileServer(http.Dir(s.distDir)))
	return m
}

// guard rejects the cross-origin POST. A localhost control plane is reachable
// from any page the human happens to have open — a form or fetch to
// http://127.0.0.1:<port>/api/... is not blocked by the browser, and the
// reply being unreadable does not un-run the mutation. So mutations require
// either no Origin (curl, the CLI) or our own.
func (s *dashServer) guard(w http.ResponseWriter, r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == s.origin {
		return true
	}
	writeErr(w, http.StatusForbidden, fmt.Sprintf("cross-origin request from %s refused", o))
	return false
}

func (s *dashServer) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
	// Reconcile before composing, so the dashboard never shows a rung the
	// filesystem has already left behind — the same call the snapshot path
	// makes, at the same point in the pipeline.
	if s.reconcile {
		reconcileProjects(s.stateDir)
	}
	writeJSON(w, http.StatusOK, dashboard.Compose(dashboard.Options{
		StateDir:       s.stateDir,
		Version:        s.version,
		SnapshotAt:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		SlugOverride:   s.slug,
		DecisionsCap:   2000,
		SkillEventsCap: 2000,
		Warn:           dashErr,
	}))
}

// projectEnv resolves {project} to the identity.Env the ticket store needs.
func (s *dashServer) projectEnv(project string) (identity.Env, error) {
	if !idRe.MatchString(project) {
		return identity.Env{}, fmt.Errorf("invalid project slug %q", project)
	}
	dir := filepath.Join(s.stateDir, "projects", project)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return identity.Env{}, fmt.Errorf("no babysit state for project %q", project)
	}
	return identity.Env{ProjectHome: dir, Slug: project}, nil
}

// ticketStore resolves {project}/{ticket} to a store for an EXISTING ticket.
// Requiring the directory to be there already is what keeps a typo'd id from
// materializing an empty ticket as a side effect of pausing it.
func (s *dashServer) ticketStore(project, id string) (*ticket.Store, error) {
	env, err := s.projectEnv(project)
	if err != nil {
		return nil, err
	}
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("invalid ticket id %q", id)
	}
	env.Ticket = id
	st := ticket.New(env)
	if fi, err := os.Stat(st.Home()); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("no ticket %q in project %q", id, project)
	}
	return st, nil
}

type createTicketReq struct {
	Project     string `json:"project"`
	Requirement string `json:"requirement"`
	SlugHint    string `json:"slug_hint"`
}

// handleCreateTicket seeds a ticket on disk: the directory, index.json with
// defaults, and requirement.md. It deliberately does NOT cut a branch the way
// `bbs ticket ensure` does — the human creating work in a browser has no
// checkout in scope, and the branch is the foreman's to cut in its own
// worktree when it picks the ticket up.
func (s *dashServer) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req createTicketReq
	if !decode(w, r, &req) {
		return
	}
	env, err := s.projectEnv(req.Project)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Requirement) == "" {
		writeErr(w, http.StatusBadRequest, "requirement is required — a ticket with no statement of the work is not actionable")
		return
	}

	env.Ticket = newTicketID()
	st := ticket.New(env)
	st.EnsureDirs()
	reqPath := filepath.Join(st.Home(), "requirement.md")
	title := req.SlugHint
	if title == "" {
		title = firstLineOf(req.Requirement)
	}
	err = withLock(st, func() error {
		if err := os.WriteFile(reqPath, []byte(strings.TrimRight(req.Requirement, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		doc := loadForMutate(st)
		doc.EnsureDefaults(env.Ticket)
		doc.Set("id", env.Ticket)
		doc.Set("title", title)
		doc.Set("pointers.requirement", "requirement.md")
		if err := ticket.WriteDoc(st.IndexPath(), doc); err != nil {
			return err
		}
		st.HistoryAppendExtra("ticket_initialized", actorRole(), "")
		st.HistoryAppendExtra("requirement_seeded", actorRole(), "")
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"ticket": env.Ticket, "home": st.Home(), "requirement": reqPath,
	})
}

type assignReq struct {
	Foreman string `json:"foreman"` // "" unassigns
}

func (s *dashServer) handleAssign(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req assignReq
	if !decode(w, r, &req) {
		return
	}
	st, err := s.ticketStore(r.PathValue("project"), r.PathValue("ticket"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Foreman != "" {
		if err := foreman.ValidID(req.Foreman); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := assignSet(st, req.Foreman, actorRole()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]interface{}{"ticket": st.Env.Ticket, "assignee": req.Foreman}
	if req.Foreman != "" {
		// The poke lands in the foreman's chat as if a human typed it, so it has
		// to read as an instruction rather than a notification.
		wake := s.wake(req.Foreman, fmt.Sprintf(
			"bbs: ticket %s was assigned to you from the dashboard — pick it up on your next tick.",
			st.Env.Ticket))
		resp["wake"] = wake.state
		resp["wake_detail"] = wake.detail
	}
	writeJSON(w, http.StatusOK, resp)
}

type controlReq struct {
	Action string `json:"action"` // pause | cancel | resume | restore
	Note   string `json:"note"`
}

func (s *dashServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req controlReq
	if !decode(w, r, &req) {
		return
	}
	st, err := s.ticketStore(r.PathValue("project"), r.PathValue("ticket"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	switch req.Action {
	case "pause", "cancel":
		state := "paused"
		if req.Action == "cancel" {
			state = "cancelled"
		}
		status, conflict, err := controlApply(st, state, req.Note, actorRole())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if conflict != "" && conflict != state {
			// 409, not 500: nothing is broken, the ticket is simply already
			// held by the other verb and the UI should offer that undo.
			writeErr(w, http.StatusConflict, fmt.Sprintf(
				"%s is already %s — clear it first (%s)", st.Env.Ticket, conflict, undoVerb(conflict)))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ticket": st.Env.Ticket, "control": state, "status": status,
		})
	case "resume", "restore":
		want := "paused"
		if req.Action == "restore" {
			want = "cancelled"
		}
		cur, status, err := controlClear(st, want, actorRole())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if cur != "" && cur != want {
			writeErr(w, http.StatusConflict, fmt.Sprintf(
				"%s is %s, not %s — use %s", st.Env.Ticket, cur, want, undoVerb(cur)))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ticket": st.Env.Ticket, "control": "", "status": status,
		})
	default:
		writeErr(w, http.StatusBadRequest, "action must be pause, cancel, resume or restore")
	}
}

type spawnReq struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Command string `json:"command"`
}

func (s *dashServer) handleSpawnForeman(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req spawnReq
	if !decode(w, r, &req) {
		return
	}
	if req.Dir == "" {
		writeErr(w, http.StatusBadRequest, "dir is required — a foreman is bound to one project folder")
		return
	}
	args := []string{req.ID, "--dir", req.Dir}
	if req.ID == "" {
		args = args[1:]
	}
	if req.Command != "" {
		args = append(args, "--command", req.Command)
	}
	id, err := foremanSpawn(args)
	if err != nil {
		// Spawn failures are the human's to read: cmux missing, token denied,
		// id already registered. All of them are 400-class — the server is
		// fine, the request cannot be satisfied as asked.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"spawned": id, "dir": req.Dir})
}

// wakeResult is what the UI shows next to an assignment: whether the poke
// landed, and if not, whose problem it is.
type wakeResult struct {
	state  string // sent | unreachable | cmux-unavailable | unknown-foreman
	detail string
}

// wake pokes a foreman's cmux workspace with the same input channel a human
// types into. It never fails the assignment: the assignment is already on disk
// and the foreman re-derives its inbox from the tickets on the next tick, so a
// failed poke is a latency problem, not a correctness one.
//
// The two failure classes are kept apart deliberately. ErrNoWorkspace means the
// workspace is gone — that is criterion 10's unreachable. A preflight failure
// means cmux itself is missing, not running, or refusing our capability token,
// which says nothing about the foreman: reporting it as "unreachable" would let
// a rotated token quietly mark every live foreman dead. Preflight runs per call
// rather than caching a Client, so it re-reads CMUX_SOCKET_CAPABILITY from the
// environment each time and a re-launched server picks up a new token.
func (s *dashServer) wake(id, msg string) wakeResult {
	rec, err := foreman.Load(id)
	if err != nil {
		return wakeResult{"unknown-foreman", err.Error()}
	}
	if rec.WorkspaceTitle == "" {
		return wakeResult{"unreachable", "foreman has no cmux workspace — it was registered, not spawned"}
	}
	client, err := cmux.Preflight()
	if err != nil {
		return wakeResult{"cmux-unavailable", err.Error()}
	}
	if err := client.Send(rec.WorkspaceTitle, msg+"\n"); err != nil {
		if errors.Is(err, cmux.ErrNoWorkspace) {
			foreman.MarkUnreachable(id)
			return wakeResult{"unreachable", fmt.Sprintf("workspace %q is not open — the assignment is on disk and will be picked up on the foreman's next resume", rec.WorkspaceTitle)}
		}
		return wakeResult{"cmux-unavailable", err.Error()}
	}
	foreman.ClearUnreachable(id)
	return wakeResult{"sent", ""}
}

// serveDashboard binds a localhost listener and serves until interrupted.
func serveDashboard(s *dashServer, port int, open bool) error {
	if _, err := os.Stat(filepath.Join(s.distDir, "index.html")); err != nil {
		dashErr("web/dist/ missing; run: bbs dashboard build")
		os.Exit(1)
	}
	// 127.0.0.1, never :: or 0.0.0.0 — this API mutates ticket state and spawns
	// terminal sessions with no authentication whatsoever. It is a local tool.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("cannot listen on 127.0.0.1:%d: %w", port, err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	s.origin = fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	fmt.Printf(retarget("bbs-dashboard: serving %s on %s\n"), s.distDir, s.origin)
	if open {
		openBrowser(s.origin + "/")
	}
	return http.Serve(ln, s.mux())
}

func writeJSON(w http.ResponseWriter, code int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decode reads a JSON body, treating an empty one as an empty object so a
// POST with no payload reaches the handler's own required-field messages
// instead of a parse error that names none of them.
func decode(w http.ResponseWriter, r *http.Request, into interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(into); err != nil && err.Error() != "EOF" {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func firstLineOf(s string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}
