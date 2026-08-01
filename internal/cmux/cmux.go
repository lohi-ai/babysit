// Package cmux drives cmux workspaces — the only terminal interface babysit
// has. There is deliberately no tmux path and no backend detection: supporting
// every multiplexer would cap the harness at the lowest common subset
// (capture-pane + send-keys) and leave status pills, notifications and
// per-workspace cwd permanently optional. Requiring cmux makes the workspace
// API something bbs can depend on.
//
// Two facts about that API were found by probe, not by reading docs, and both
// shape this package (evidence: the parent ticket's evidence/spike/):
//
//   - The socket authenticates on a BEARER TOKEN, not on process ancestry.
//     CMUX_SOCKET_CAPABILITY alone is sufficient; CMUX_SOCKET_PATH alone is
//     denied. A detached process (ppid=1, no tty) carrying only the capability
//     can create, send, rename, notify, capture and close. So a server *can*
//     drive cmux while detached — but it cannot scrub its environment, which is
//     why Preflight checks the token as its own step with its own message.
//
//   - `cmux send` is BYTE-TRANSPARENT. It forwards bytes verbatim and does not
//     interpret bracketed-paste markers. Multi-line payloads arrive with real
//     newlines and without per-line submission, so wrapping text in
//     \033[200~…\033[201~ does not signal a paste — it injects those literal
//     bytes into the recipient's input. Everything here sends raw; see Send.
package cmux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The three Preflight failures are distinct errors because their fixes are
// distinct: install cmux, hand the process a token, start the app. Collapsing
// them into one "cmux unavailable" is what makes the missing-token case — cmux
// installed, running, and denying every call — unfixable from the message.
var (
	ErrNotInstalled  = errors.New("cmux is not installed")
	ErrNoCapability  = errors.New("no cmux socket capability")
	ErrNotResponding = errors.New("cmux is not responding")
)

// Client is a checked cmux binary. Always build one through Preflight — the
// zero value cannot run anything.
type Client struct {
	bin string
}

// Preflight returns a usable Client or the reason there isn't one.
//
// The socket accepts a caller on EITHER of two grounds: process ancestry
// ("only processes started inside cmux can connect"), or the capability token.
// So the token is checked only to EXPLAIN a denial, never as a precondition —
// gating on it up front would refuse to run inside a cmux terminal that simply
// does not export the variable, which cmux itself would have served. Probed
// both ways: a fully detached process (ppid=1, ancestry broken) is denied
// without the token and served with it; a cmux descendant is served with no
// CMUX_* environment at all.
func Preflight() (*Client, error) {
	bin, err := exec.LookPath("cmux")
	if err != nil {
		return nil, fmt.Errorf("%w — install it from https://cmux.sh, or add its CLI to PATH", ErrNotInstalled)
	}
	c := &Client{bin: bin}

	out, err := c.run("ping")
	if err == nil && strings.TrimSpace(out) == "PONG" {
		return c, nil
	}
	detail := firstLine(strings.TrimSpace(out + errText(err)))

	if os.Getenv("CMUX_SOCKET_CAPABILITY") == "" {
		return nil, fmt.Errorf("%w: cmux is running but denied us (%s). A process that was not started "+
			"inside cmux authenticates with the socket token, and CMUX_SOCKET_CAPABILITY is unset. "+
			"Start bbs from a cmux workspace, or pass the token: CMUX_SOCKET_CAPABILITY=<token> bbs …",
			ErrNoCapability, detail)
	}
	return nil, fmt.Errorf("%w (ping did not return PONG: %s) — is the cmux app open?",
		ErrNotResponding, detail)
}

// Window is one cmux window. ID is the UUID; commands also accept "window:<n>"
// or the bare index, so any of the three fields is a valid handle.
type Window struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
}

// Workspace is one workspace inside a window. Ref ("workspace:<n>") is what
// commands take, but it CHURNS as workspaces open and close — never store it.
// CustomTitle is the durable handle; see workspaceRef.
type Workspace struct {
	Ref string `json:"ref"`
	// CustomTitle is what `create --name` set, and it only changes when
	// something renames the workspace. Title is what the UI displays, which
	// for an un-named workspace is generated from the agent's current
	// activity ("✳ Commit all changes") and changes on its own — never a
	// handle. HasCustom says which of the two Title is showing.
	CustomTitle string `json:"custom_title"`
	Title       string `json:"title"`
	HasCustom   bool   `json:"has_custom_title"`
	Index       int    `json:"index"`
	Directory   string `json:"current_directory"`
}

// ListWindows returns every window. It reads `--json` rather than the plain
// listing on purpose: the text form prefixes the selected row with "* ", so
// splitting a whole line yields a shifted field set for exactly one window —
// the kind of bug that only shows up once the human selects a different window.
func (c *Client) ListWindows() ([]Window, error) {
	out, err := c.run("list-windows", "--json")
	if err != nil {
		return nil, err
	}
	var ws []Window
	if err := json.Unmarshal([]byte(out), &ws); err != nil {
		return nil, fmt.Errorf("cmux list-windows: %w", err)
	}
	return ws, nil
}

// ListWorkspaces returns the workspaces of one window; an empty window handle
// means the current one.
func (c *Client) ListWorkspaces(window string) ([]Workspace, error) {
	args := []string{"workspace", "list", "--json"}
	if window != "" {
		args = append(args, "--window", window)
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil, fmt.Errorf("cmux workspace list: %w", err)
	}
	return payload.Workspaces, nil
}

// CreateOpts describes a workspace to create. Command runs in the new
// workspace's shell — this is how a foreman session gets started.
type CreateOpts struct {
	Title   string
	Cwd     string
	Command string
	Window  string
}

// Create makes a workspace and returns its ref. The ref comes from a fresh
// lookup by title rather than from create's stdout, so Create and every later
// call resolve the same way and there is one parsing path to be wrong.
func (c *Client) Create(o CreateOpts) (string, error) {
	// create defaults to "the caller's window" — which a detached server does
	// not have, so it must name one explicitly or the call fails with no
	// window context. Any window will do; the first is stable enough.
	window := o.Window
	if window == "" {
		windows, err := c.ListWindows()
		if err != nil {
			return "", err
		}
		if len(windows) == 0 {
			return "", fmt.Errorf("cmux workspace create: no open windows to create in")
		}
		window = windows[0].ID
	}
	args := []string{"workspace", "create", "--name", o.Title, "--window", window}
	if o.Cwd != "" {
		args = append(args, "--cwd", o.Cwd)
	}
	if o.Command != "" {
		args = append(args, "--command", o.Command)
	}
	if _, err := c.run(args...); err != nil {
		return "", err
	}
	return c.workspaceRef(o.Title)
}

// Send writes text into a workspace's input, exactly as if it were typed.
//
// The payload is passed RAW. cmux send is byte-transparent: it does not
// interpret bracketed-paste markers, so wrapping a multi-line block in
// \033[200~…\033[201~ injects those literal bytes instead of signalling a
// paste. A multi-line block arrives with real newlines and is not submitted
// per line, which makes the whole protocol for pasting a block into an agent
// session: Send("ctrl+u" clear) → Send(block) → Send("\n").
//
// The one thing it does read is backslash escapes: \n and \r send Enter, \t
// sends Tab. Those CANNOT be escaped — probed: sending `a\\nb` yields a
// literal backslash followed by a real newline, because the first backslash
// passes through unrecognized and the second still pairs with the n. So text
// containing a literal \n, \r or \t sequence (a code snippet, a regex) cannot
// be delivered intact by any quoting, and a payload carrying one submits
// early. Callers sending such content must write it to a file and send a path.
//
// Send never needs \n for submission: Enter is SendKey's job.
func (c *Client) Send(title, text string) error {
	ref, err := c.workspaceRef(title)
	if err != nil {
		return err
	}
	_, err = c.run("send", "--workspace", ref, "--", text)
	return err
}

// SendKey presses a key ("enter", "ctrl+u") in a workspace. Keys go through
// their own verb, which is what lets Send stay a pure text channel: the paste
// protocol for an agent session is SendKey("ctrl+u") — clear whatever the
// input already holds, since blind-Entering a pre-filled suggestion runs the
// wrong thing — then Send(block), then SendKey("enter").
func (c *Client) SendKey(title, key string) error {
	ref, err := c.workspaceRef(title)
	if err != nil {
		return err
	}
	_, err = c.run("send-key", "--workspace", ref, key)
	return err
}

// Notify raises a cmux notification on a workspace.
func (c *Client) Notify(title, subject, body string) error {
	ref, err := c.workspaceRef(title)
	if err != nil {
		return err
	}
	_, err = c.run("notify", "--workspace", ref, "--title", subject, "--body", body)
	return err
}

// Lanes are the ONLY status values cmux accepts — anything else is rejected
// with invalid_params. Any babysit status vocabulary has to map onto these
// five (plus "auto", which releases the pin) rather than invent its own.
var Lanes = []string{"todo", "working", "needs-attention", "review", "done", "auto"}

// SetStatus pins a workspace's status pill.
func (c *Client) SetStatus(title, lane string) error {
	if !contains(Lanes, lane) {
		return fmt.Errorf("cmux status %q: must be one of %s", lane, strings.Join(Lanes, ", "))
	}
	ref, err := c.workspaceRef(title)
	if err != nil {
		return err
	}
	_, err = c.run("workspace", "status", "set", lane, "--workspace", ref)
	return err
}

// CapturePane returns the last n lines of a workspace's terminal — how a
// foreman reads what its worker is doing.
func (c *Client) CapturePane(title string, lines int) (string, error) {
	ref, err := c.workspaceRef(title)
	if err != nil {
		return "", err
	}
	return c.run("capture-pane", "--workspace", ref, "--lines", fmt.Sprint(lines))
}

// Close closes a workspace. Missing is not an error: closing something already
// gone is the outcome the caller wanted.
func (c *Client) Close(title string) error {
	ref, err := c.workspaceRef(title)
	if errors.Is(err, ErrNoWorkspace) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = c.run("workspace", "close", ref)
	return err
}

// ErrNoWorkspace means no workspace currently carries that title.
var ErrNoWorkspace = errors.New("no cmux workspace with that title")

// workspaceRef resolves a title to a live ref, scanning every window. It runs
// on EVERY call rather than caching, because refs are positional and churn as
// workspaces open and close — a cached "workspace:9" silently becomes some
// other agent's session, and a send would land in the wrong terminal.
func (c *Client) workspaceRef(title string) (string, error) {
	windows, err := c.ListWindows()
	if err != nil {
		return "", err
	}
	if len(windows) == 0 {
		windows = []Window{{}} // no window handle = the current one
	}
	for _, w := range windows {
		spaces, err := c.ListWorkspaces(w.ID)
		if err != nil {
			continue // a window can close between the two calls
		}
		for _, s := range spaces {
			// custom_title only — s.Title for an un-named workspace is
			// generated from agent activity and would match by coincidence.
			if s.CustomTitle == title {
				return s.Ref, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %q", ErrNoWorkspace, title)
}

func (c *Client) run(args ...string) (string, error) {
	cmd := exec.Command(c.bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("cmux %s: %s", args[0], firstLine(msg))
	}
	return string(out), nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return " " + err.Error()
}
