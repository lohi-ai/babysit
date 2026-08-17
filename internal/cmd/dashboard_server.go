package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/dashboard"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/orca"
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
	stateDir string
	// distFS is the SPA to serve: web/dist on disk when a checkout has built
	// it, else the copy embedded in the binary (internal/webui). distDir is the
	// disk path, kept only for the message that names what is being served —
	// it is empty when the embedded copy won.
	distFS  fs.FS
	distDir string
	version string
	slug    string // deprecated --slug filter, honored here so both modes agree
	// currentSlug is the project of the launch cwd — meta.active_project, so
	// the SPA opens on the repo the human started the server from.
	currentSlug string
	// currentDir is the launch cwd's repo root — meta.current_dir, which the
	// spawn form prefills as the foreman's workspace folder.
	currentDir string
	reconcile  bool
	origin     string // "http://127.0.0.1:<port>", filled in once listening
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
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/status", s.handleSetStatus)
	m.HandleFunc("DELETE /api/tickets/{project}/{ticket}", s.handleDeleteTicket)
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/control", s.handleControl)
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/approval", s.handleApproval)
	m.HandleFunc("POST /api/tickets/{project}/{ticket}/approval/comment", s.handleApprovalComment)
	m.HandleFunc("GET /api/tickets/{project}/{ticket}/prototype", s.handlePrototype)
	m.HandleFunc("POST /api/foremen", s.handleSpawnForeman)
	m.HandleFunc("POST /api/foremen/{id}/retire", s.handleRetireForeman)

	// index.html loads ./data.js unconditionally, and web/dist/data.js is
	// whatever the last snapshot run left there. Serving it here would boot the
	// SPA in snapshot mode against stale state; serving nothing leaves
	// window.__BBS_DATA__ undefined, which is exactly the signal the source
	// switch reads to take the fetch path.
	m.HandleFunc("GET /data.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintln(w, "// served mode: the SPA fetches /api/snapshot")
	})
	m.Handle("/", http.FileServerFS(s.distFS))
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
		CurrentSlug:    s.currentSlug,
		CurrentDir:     s.currentDir,
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
// The existence check is index.json rather than the directory: an empty ticket
// dir is what a crashed or half-failed create leaves behind, and accepting one
// would let a mutation finish materializing the phantom ticket it was supposed
// to refuse.
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
	if _, err := os.Stat(st.IndexPath()); err != nil {
		return nil, fmt.Errorf("no ticket %q in project %q", id, project)
	}
	return st, nil
}

// ticketReq is the opening every ticket-mutating handler shares: the
// origin/CSRF guard, then the request body, then the store for {project}/
// {ticket}. Order matters and is the order the handlers already used — a
// request that is both cross-origin and malformed must still be refused as
// cross-origin. Pass into=nil for a handler with no body. ok=false means the
// response is already written and the handler must return.
func (s *dashServer) ticketReq(w http.ResponseWriter, r *http.Request, into interface{}) (*ticket.Store, bool) {
	if !s.guard(w, r) {
		return nil, false
	}
	if into != nil && !decode(w, r, into) {
		return nil, false
	}
	st, err := s.ticketStore(r.PathValue("project"), r.PathValue("ticket"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return st, true
}

type createTicketReq struct {
	Project     string `json:"project"`
	Requirement string `json:"requirement"`
	Title       string `json:"title"` // defaults to the requirement's first line
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
	st.EnsureDirs() // the lock file lives in the ticket dir, so this precedes it
	reqPath := filepath.Join(st.Home(), "requirement.md")
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = firstLineOf(req.Requirement)
	}
	err = withLock(st, func() error {
		if err := os.WriteFile(reqPath, []byte(requirementBody(title, req.Requirement)), 0o644); err != nil {
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
	var req assignReq
	st, ok := s.ticketReq(w, r, &req)
	if !ok {
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
			"ticket %s was assigned to you from the dashboard — pick it up on your next tick.",
			st.Env.Ticket))
		resp["wake"] = wake.state
		resp["wake_detail"] = wake.detail
	}
	writeJSON(w, http.StatusOK, resp)
}

type statusReq struct {
	Status string `json:"status"`
}

// handleSetStatus moves a ticket along the lifecycle ladder by hand. It is the
// same write `bbs ticket set-status` makes, validated against the same enum —
// including the rungs a reconcile would otherwise recompute, because a human
// correcting a mis-derived status is exactly what this is for.
//
// No wake: unlike pause, a status edit does not ask the assignee to stop what it
// is doing, and it re-reads the ticket on its next tick anyway.
func (s *dashServer) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req statusReq
	if !decode(w, r, &req) {
		return
	}
	if !statusEnum[req.Status] {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf(
			"invalid status %q — valid: %s", req.Status, statusValid))
		return
	}
	st, err := s.ticketStore(r.PathValue("project"), r.PathValue("ticket"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := statusSet(st, req.Status, actorRole()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"ticket": st.Env.Ticket, "status": req.Status,
	})
}

// handleDeleteTicket moves the ticket's directory to <stateDir>/trash/<project>/
// rather than unlinking it. A ticket dir is the entire record of the work —
// requirement, plan, handoffs, verdicts, evidence — and this is the first thing
// in babysit that removes one, so the destructive version of it is not worth
// shipping for the sake of skipping a rename. Compose only scans
// projects/<slug>/tickets, so a trashed ticket is gone from the dashboard while
// still being recoverable by hand.
//
// Branches and worktrees are deliberately untouched: they live in the repo, not
// in babysit state, and reaching into a checkout to delete a branch from a
// dashboard button is a blast radius this does not want.
func (s *dashServer) handleDeleteTicket(w http.ResponseWriter, r *http.Request) {
	st, ok := s.ticketReq(w, r, nil)
	if !ok {
		return
	}
	dest, err := trashTicket(s.stateDir, r.PathValue("project"), st)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"deleted": st.Env.Ticket, "trash": dest,
	})
}

// trashTicket renames the ticket home under the lock, so a mutation already in
// flight finishes before the directory moves out from under it. The release
// afterwards targets the old path and is a no-op; the lock that travelled with
// the directory is dropped from the copy, or a restored ticket would look held
// by a writer that exited hours ago.
func trashTicket(stateDir, project string, st *ticket.Store) (string, error) {
	dest := filepath.Join(stateDir, "trash", project,
		st.Env.Ticket+"-"+time.Now().UTC().Format("20060102T150405Z"))
	err := withLock(st, func() error {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.Rename(st.Home(), dest); err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(dest, ".index.lock"))
	})
	if err != nil {
		return "", err
	}
	return dest, nil
}

type controlReq struct {
	Action string `json:"action"` // pause | cancel | resume | restore
	Note   string `json:"note"`
}

func (s *dashServer) handleControl(w http.ResponseWriter, r *http.Request) {
	var req controlReq
	st, ok := s.ticketReq(w, r, &req)
	if !ok {
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
		// Pause is the urgent one: the foreman may be dispatching this ticket
		// right now, and its next tick is too late. Same channel as assign, and
		// the same non-fatal treatment — the control state is already on disk.
		s.wakeAssignee(st, fmt.Sprintf(
			"ticket %s was %s from the dashboard — stop work on it.", st.Env.Ticket, state))
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

type approvalReq struct {
	Action string `json:"action"` // approve | redirect | drop
	Note   string `json:"note"`
}

// handleApproval is the other end of `bbs ticket approval await`: the worker is
// blocked on the record, and this is the write that unblocks it. The wake is
// what turns a 10-second poll into an immediate resume, and — as everywhere
// else — it is best-effort, because the verdict is already on disk.
func (s *dashServer) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req approvalReq
	st, ok := s.ticketReq(w, r, &req)
	if !ok {
		return
	}
	state, missing, err := approvalResolve(st, req.Action, req.Note, actorRole())
	if err != nil {
		// Bad verb, or a redirect with nothing to redirect to: the request is
		// wrong, not the server.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if missing {
		// 409 the way a double-pause does: two tabs open on the same decision,
		// and the second one is answering a question already answered.
		writeErr(w, http.StatusConflict, fmt.Sprintf(
			"%s has no approval pending — it may have been decided in another tab", st.Env.Ticket))
		return
	}
	s.wakeAssignee(st, fmt.Sprintf(
		"the plan/prototype decision on %s is in — %s. Run `bbs ticket approval status` and continue.",
		st.Env.Ticket, state))
	writeJSON(w, http.StatusOK, map[string]string{"ticket": st.Env.Ticket, "approval": state})
}

// handleApprovalComment anchors one piece of feedback to one paragraph or one
// element. Unlike a decision it does not end the wait, so there is no wake here:
// the human is mid-review, and poking the worker on every comment would resume
// it against a half-written redirect.
func (s *dashServer) handleApprovalComment(w http.ResponseWriter, r *http.Request) {
	var req approvalNote
	st, ok := s.ticketReq(w, r, &req)
	if !ok {
		return
	}
	id, missing, err := approvalAddComment(st, req, actorRole())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if missing {
		writeErr(w, http.StatusConflict, fmt.Sprintf(
			"%s has no approval pending — it may have been decided in another tab", st.Env.Ticket))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ticket": st.Env.Ticket, "comment": id})
}

// handlePrototype serves the mock as a real document so `Open in new tab` has
// somewhere to go. The snapshot already embeds the HTML for the inline frame;
// this exists because an iframe's srcdoc has no URL a human can open, share, or
// reload at full width.
func (s *dashServer) handlePrototype(w http.ResponseWriter, r *http.Request) {
	st, err := s.ticketStore(r.PathValue("project"), r.PathValue("ticket"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	path := filepath.Join(st.Home(), "prototype.html")
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("%s has no prototype.html", st.Env.Ticket))
		return
	}
	// The mock is untrusted markup written by a worker, served same-origin next
	// to an API that mutates tickets. CSP keeps it from reaching back at that
	// API; the sandbox drops the ambient authority the same origin would grant.
	w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' data:; script-src 'unsafe-inline'; connect-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, path)
}

type spawnReq struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Command string `json:"command"`
	// Agent is optional: omitted means resolve from config, which is what the
	// dashboard's spawn button sends.
	Agent string `json:"agent"`
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
	id, err := spawnForeman(req.ID, req.Dir, req.Command, req.Agent)
	if err != nil {
		// Spawn failures are the human's to read: Orca missing, app closed,
		// id already registered. All of them are 400-class — the server is
		// fine, the request cannot be satisfied as asked.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"spawned": id, "dir": req.Dir})
}

type retireReq struct {
	// KeepWorkspace leaves the Orca terminal open. Default false matches the CLI:
	// retiring is normally how you clear a foreman that is already gone, and
	// leaving its pane behind is the thing that made it confusing.
	KeepWorkspace bool `json:"keep_workspace"`
}

// handleRetireForeman is the counterpart to spawn, and the only way to clear a
// foreman record from the dashboard. Records are never garbage-collected — a
// foreman that died hours ago stays in the list forever, still holding whatever
// was assigned to it — so without this the list only ever grows and the human
// has to drop to the CLI to tidy it.
//
// It does NOT check whether the foreman still holds open tickets. That check
// belongs to the UI, which can name them in the confirm; enforcing it here would
// block the one case this exists for — a dead foreman whose tickets are stuck
// precisely because it is dead.
func (s *dashServer) handleRetireForeman(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	// No id validation here on purpose: foreman.Load validates before it builds a
	// path, so every route into the store already refuses an illegal id with the
	// message that names the rule. A second check here would be a copy that can
	// only ever drift from the one doing the work.
	id := r.PathValue("id")
	var req retireReq
	if !decode(w, r, &req) {
		return
	}
	msg, err := retireForeman(id, req.KeepWorkspace)
	if err != nil {
		// Same 400-class reasoning as spawn: an unknown id or an Orca close that
		// was refused is the request's problem, not the server's.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"retired": id, "detail": msg})
}

// wakeResult is what the UI shows next to an assignment: whether the poke
// landed, and if not, whose problem it is.
type wakeResult struct {
	state  string // sent | unreachable | orca-unavailable | unknown-foreman
	detail string
}

// wakeAssignee pokes whoever owns the ticket, if anyone does. Used by the
// control verbs, where the foreman is not named in the request the way it is
// in an assignment.
func (s *dashServer) wakeAssignee(st *ticket.Store, msg string) {
	id := ticket.ReadDoc(st.IndexPath()).Get("assignee")
	if id == "" {
		return
	}
	s.wake(id, msg)
}

// wake pokes a foreman's Orca terminal with the same input channel a human
// types into. It never fails the assignment: the assignment is already on disk
// and the foreman re-derives its inbox from the tickets on the next tick, so a
// failed poke is a latency problem, not a correctness one.
//
// Every poke is prefixed with the foreman skill invocation rather than plain
// prose. A foreman is a long-lived session, so by the time the dashboard pokes
// it the skill may be several compactions behind it; re-invoking reloads the
// protocol the message assumes the receiver is following. Callers pass the
// instruction alone — the prefix is applied here so no wake site can forget it.
//
// The two failure classes are kept apart deliberately. ErrNoTerminal means the
// terminal is gone — that is criterion 10's unreachable. A preflight failure
// means Orca itself is missing or not running, which says nothing about the
// foreman: reporting it as "unreachable" would let a closed app quietly mark
// every live foreman dead. Preflight runs per call rather than caching a
// Client, so a re-launched server picks up a newly opened Orca.
func (s *dashServer) wake(id, msg string) wakeResult {
	rec, err := foreman.Load(id)
	if err != nil {
		return wakeResult{"unknown-foreman", err.Error()}
	}
	if rec.WorkspaceTitle == "" {
		return wakeResult{"unreachable", "foreman has no Orca terminal — it was registered, not spawned"}
	}
	client, err := orca.Preflight()
	if err != nil {
		return wakeResult{"orca-unavailable", err.Error()}
	}
	if err := client.SendEnter(rec.WorkspaceTitle, "/bbs:foreman "+msg); err != nil {
		if errors.Is(err, orca.ErrNoTerminal) {
			foreman.MarkUnreachable(id)
			return wakeResult{"unreachable", fmt.Sprintf("terminal %q is not open — the assignment is on disk and will be picked up on the foreman's next resume", rec.WorkspaceTitle)}
		}
		return wakeResult{"orca-unavailable", err.Error()}
	}
	foreman.ClearUnreachable(id)
	return wakeResult{"sent", ""}
}

// serveDashboard binds a localhost listener and serves until interrupted.
func serveDashboard(s *dashServer, port int, open bool) error {
	if s.distFS == nil {
		// Same string as the snapshot path's, verbatim: `bbs dashboard` with no
		// flags now lands here instead of there, and that is the message
		// tests/test_bbs_dashboard.sh asserts on for the default invocation.
		dashErr("web/dist/ missing; run: bbs-dashboard build")
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

	src := s.distDir
	if src == "" {
		src = "the embedded dashboard"
	}
	fmt.Printf(retarget("bbs-dashboard: serving %s on %s\n"), src, s.origin)
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

// requirementBody is the file the ticket is listed by. dashboard.Compose takes
// a ticket's display title from requirement.md's first `# ` heading and falls
// back to the bare id — index.json's `title` is never read for display — so a
// requirement typed into the browser with no heading would list as "bs-xxxxxxxx"
// in the very dashboard that created it. Bodies that already open with a
// heading are left exactly as typed.
func requirementBody(title, requirement string) string {
	body := strings.TrimRight(requirement, "\n") + "\n"
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "# ") {
			return body
		}
	}
	return "# " + title + "\n\n" + body
}

func firstLineOf(s string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	// Cut on a rune boundary — babysit's own requirements are full of em-dashes
	// and arrows, and a byte cut mid-sequence stores a U+FFFD in the title.
	if r := []rune(line); len(r) > 120 {
		line = string(r[:120])
	}
	return line
}
