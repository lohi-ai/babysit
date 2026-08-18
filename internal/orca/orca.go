// Package orca drives Orca terminals — the only terminal interface babysit
// has. There is deliberately no cmux path and no backend detection: supporting
// every multiplexer would cap the harness at the lowest common subset
// (capture-pane + send-keys) and leave the sidebar, comments and live
// terminals permanently optional. Requiring Orca makes the terminal API
// something bbs can depend on.
//
// Orca's CLI is JSON-first. Every call here uses --json and reads
// result.terminal.handle / result.terminals[].title. The durable handle is
// the terminal title (spawn sets "bbs foreman <id>"); the runtime handle
// (term_…) is what later calls take, and it is re-derived from the title
// on every use because handles are issued per runtime session.
package orca

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"
)

var (
	ErrNotInstalled  = errors.New("orca is not installed")
	ErrNotResponding = errors.New("orca is not responding")
	// ErrNoTerminal means no live terminal currently carries that title.
	ErrNoTerminal = errors.New("no orca terminal with that title")
)

// Client is a checked orca binary. Always build one through Preflight — the
// zero value cannot run anything.
type Client struct {
	bin string
}

// Preflight returns a usable Client or the reason there isn't one.
//
// The three failures are distinct because their fixes are distinct: install
// Orca, start the app, or wait for the runtime. Collapsing them into one
// "orca unavailable" is what makes "installed but the app is closed"
// unfixable from the message.
func Preflight() (*Client, error) {
	bin, err := lookPath()
	if err != nil {
		return nil, fmt.Errorf("%w — install it from https://www.onorca.dev and make sure the app is running", ErrNotInstalled)
	}
	c := &Client{bin: bin}
	if err := c.ready(); err == nil {
		return c, nil
	}
	// status failed: try opening the app, then status again. open is a no-op
	// when the app is already up; it is the only way a detached bbs process
	// can recover from "Orca is installed but not running".
	_, _ = c.run("open")
	if err := c.ready(); err != nil {
		return nil, fmt.Errorf("%w — is the Orca app open? (%s)", ErrNotResponding, firstLine(err.Error()))
	}
	return c, nil
}

func lookPath() (string, error) {
	if cmd := os.Getenv("ORCA_CLI_COMMAND"); cmd != "" {
		return exec.LookPath(cmd)
	}
	if os.Getenv("ORCA_DEV_REPO_ROOT") != "" {
		if p, err := exec.LookPath("orca-dev"); err == nil {
			return p, nil
		}
	}
	// On Linux outside an Orca-managed terminal, bare `orca` is usually the
	// GNOME screen reader. Prefer orca-ide there.
	if runtime.GOOS == "linux" && os.Getenv("ORCA_RUNTIME_ID") == "" {
		if p, err := exec.LookPath("orca-ide"); err == nil {
			return p, nil
		}
	}
	return exec.LookPath("orca")
}

func (c *Client) ready() error {
	raw, err := c.run("status")
	if err != nil {
		return err
	}
	var st struct {
		Runtime struct {
			Reachable bool `json:"reachable"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if !st.Runtime.Reachable {
		return errors.New("runtime not reachable")
	}
	return nil
}

// Terminal is one live Orca terminal.
type Terminal struct {
	Handle       string `json:"handle"`
	Title        string `json:"title"`
	Connected    bool   `json:"connected"`
	WorktreePath string `json:"worktreePath"`
}

// CreateOpts describes a terminal to create. Command runs in the new
// terminal — this is how a foreman session gets started.
type CreateOpts struct {
	Title   string
	Cwd     string
	Command string
}

// Create makes a terminal in the repo's existing Orca worktree and returns
// its runtime handle. It does not create a git worktree — babysit still
// owns isolation via --mode=worktree.
func (c *Client) Create(o CreateOpts) (string, error) {
	// Register the repo if Orca does not already track it. add is a no-op
	// when the path is already known; a failure here is not fatal because
	// the path may already be an Orca worktree.
	if o.Cwd != "" {
		_, _ = c.run("repo", "add", "--path", o.Cwd)
	}
	args := []string{"terminal", "create", "--title", o.Title}
	if o.Cwd != "" {
		args = append(args, "--worktree", "path:"+o.Cwd)
	}
	if o.Command != "" {
		args = append(args, "--command", o.Command)
	}
	raw, err := c.run(args...)
	if err != nil {
		return "", err
	}
	if h := handleFrom(raw); h != "" {
		return h, nil
	}
	// Older CLIs may not echo the handle. Re-derive the same way every later
	// call does, so Create and Ref share one parsing path.
	return c.Ref(o.Title)
}

// Send writes text into a terminal's input without submitting it.
func (c *Client) Send(title, text string) error {
	handle, err := c.Ref(title)
	if err != nil {
		return err
	}
	_, err = c.run("terminal", "send", "--terminal", handle, "--text", text)
	return err
}

// SendEnter writes text and submits it. Text with no Enter sits in the
// worker's composer unsent while the pane still looks busy.
func (c *Client) SendEnter(title, text string) error {
	handle, err := c.Ref(title)
	if err != nil {
		return err
	}
	_, err = c.run("terminal", "send", "--terminal", handle, "--text", text, "--enter")
	return err
}

// SendKey presses a small set of keys Orca can deliver. "enter" submits;
// "interrupt" is the wedged-TUI recovery (ctrl+c). Anything else is rejected
// here rather than forwarded — Orca has no arrow send-key.
func (c *Client) SendKey(title, key string) error {
	handle, err := c.Ref(title)
	if err != nil {
		return err
	}
	switch key {
	case "enter":
		_, err = c.run("terminal", "send", "--terminal", handle, "--enter")
	case "interrupt", "ctrl+c":
		_, err = c.run("terminal", "send", "--terminal", handle, "--interrupt")
	default:
		return fmt.Errorf("orca send-key %q: only enter and interrupt are supported", key)
	}
	return err
}

// Notify stores a comment on the terminal's worktree so the card announces
// itself. Orca has no toast API; the comment is the closest equivalent.
func (c *Client) Notify(title, subject, body string) error {
	term, err := c.lookup(title)
	if err != nil {
		return err
	}
	comment := subject
	if body != "" {
		comment = subject + " — " + firstLine(body)
	}
	_, err = c.run("worktree", "set", "--worktree", worktreeSel(term), "--comment", comment)
	return err
}

// statusMap is the cmux-era lane vocabulary onto Orca board columns. Watch
// and the skill still speak the old names; Orca rejects anything else.
var statusMap = map[string]string{
	"todo":            "todo",
	"working":         "in-progress",
	"needs-attention": "in-review",
	"review":          "in-review",
	"done":            "completed",
	"auto":            "in-progress",
	"in-progress":     "in-progress",
	"in-review":       "in-review",
	"completed":       "completed",
}

// SetStatus pins the worktree's board column. Per-terminal signal is the
// title; this is the batch-level pill.
func (c *Client) SetStatus(title, lane string) error {
	mapped, ok := statusMap[lane]
	if !ok {
		return fmt.Errorf("orca status %q: must be one of todo, working, needs-attention, review, done", lane)
	}
	term, err := c.lookup(title)
	if err != nil {
		return err
	}
	_, err = c.run("worktree", "set", "--worktree", worktreeSel(term), "--workspace-status", mapped)
	return err
}

// CapturePane returns the last n lines of a terminal — how a watch reads
// what a foreman is doing.
func (c *Client) CapturePane(title string, lines int) (string, error) {
	handle, err := c.Ref(title)
	if err != nil {
		return "", err
	}
	raw, err := c.run("terminal", "read", "--terminal", handle, "--limit", fmt.Sprint(lines))
	if err != nil {
		return "", err
	}
	var wrap struct {
		Terminal struct {
			Tail []string `json:"tail"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return "", fmt.Errorf("terminal read: %w", err)
	}
	return strings.Join(wrap.Terminal.Tail, "\n"), nil
}

// Close closes a terminal tab. Missing is not an error: closing something
// already gone is the outcome the caller wanted.
func (c *Client) Close(title string) error {
	handle, err := c.Ref(title)
	if errors.Is(err, ErrNoTerminal) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = c.run("terminal", "close", "--terminal", handle, "--tab")
	return err
}

// Ref resolves a terminal title to its live handle, or ErrNoTerminal when
// no open terminal carries it. It is the read a caller uses to decide
// whether a recorded terminal is still there — probing with a Send would
// answer the same question by typing into somebody's session.
func (c *Client) Ref(title string) (string, error) {
	t, err := c.lookup(title)
	if err != nil {
		return "", err
	}
	return t.Handle, nil
}

func (c *Client) lookup(title string) (Terminal, error) {
	terms, err := c.list()
	if err != nil {
		return Terminal{}, err
	}
	for _, t := range terms {
		if t.Title == title {
			return t, nil
		}
	}
	return Terminal{}, fmt.Errorf("%w: %q", ErrNoTerminal, title)
}

// LookupSpawn finds the tab for a spawn-goal / spawn-review. Orca often
// rewrites the title we created (e.g. "bbs review bs-x1" → "bs-x1 plan and
// prototype review"), so a live tab is matched by ticket + kind, not exact
// title.
//
// Both halves are matched as whole words. Substring matching is what makes a
// loose match dangerous rather than merely loose: `bs-x1` would claim the tab
// for `bs-x11`, and a caller that mistakes another ticket's tab for its own
// reports ALREADY_RUNNING and never starts the worker at all.
func (c *Client) LookupSpawn(kind, ticket string) (Terminal, error) {
	terms, err := c.list()
	if err != nil {
		return Terminal{}, err
	}
	exact := "bbs " + kind + " " + ticket
	for _, t := range terms {
		if t.Title == exact {
			return t, nil
		}
	}
	for _, t := range terms {
		if titleHasWord(t.Title, ticket) && titleHasWord(t.Title, kind) {
			return t, nil
		}
	}
	return Terminal{}, fmt.Errorf("%w: %s %s", ErrNoTerminal, kind, ticket)
}

// titleHasWord reports whether title contains word as a whole token, comparing
// case-insensitively and treating every non-alphanumeric rune as a separator so
// a retitle that repunctuates ("bs-x1: review") still matches.
func titleHasWord(title, word string) bool {
	if word == "" {
		return false
	}
	sep := func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	}
	for _, f := range strings.FieldsFunc(title, sep) {
		if strings.EqualFold(f, word) {
			return true
		}
	}
	return false
}

func (c *Client) list() ([]Terminal, error) {
	raw, err := c.run("terminal", "list")
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Terminals []Terminal `json:"terminals"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("terminal list: %w", err)
	}
	return wrap.Terminals, nil
}

func handleFrom(raw json.RawMessage) string {
	var wrap struct {
		Handle   string `json:"handle"`
		Terminal struct {
			Handle string `json:"handle"`
		} `json:"terminal"`
	}
	_ = json.Unmarshal(raw, &wrap)
	if wrap.Handle != "" {
		return wrap.Handle
	}
	return wrap.Terminal.Handle
}

func worktreeSel(t Terminal) string {
	if t.WorktreePath != "" {
		return "path:" + t.WorktreePath
	}
	return "active"
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) run(args ...string) (json.RawMessage, error) {
	full := append(append([]string{}, args...), "--json")
	cmd := exec.Command(c.bin, full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	msg := strings.TrimSpace(stderr.String())
	if len(out) > 0 {
		var env envelope
		if jerr := json.Unmarshal(out, &env); jerr == nil {
			if env.OK {
				if len(env.Result) == 0 {
					return json.RawMessage(`{}`), nil
				}
				return env.Result, nil
			}
			if env.Error != nil && env.Error.Message != "" {
				msg = env.Error.Message
			}
		}
	}
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("orca %s: %s", args[0], firstLine(msg))
	}
	if msg != "" {
		return nil, fmt.Errorf("orca %s: %s", args[0], firstLine(msg))
	}
	// Non-envelope stdout: treat the whole body as the result so a stub that
	// prints a bare object still works.
	if len(out) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(out), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
