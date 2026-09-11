package orca

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOrca puts a stub `orca` on PATH whose behavior is the given script body
// and returns its call log path. Preflight shells out, so a stub binary is
// the only way to exercise this without a running Orca app.
func fakeOrca(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\nPATH=/bin:/usr/bin\nprintf '%s\\n' \"$*\" >> " + log + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "orca"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ORCA_CLI_COMMAND", "")
	t.Setenv("ORCA_DEV_REPO_ROOT", "")
	return log
}

func readyBody() string {
	return `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"state":"ready"}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
esac`
}

func TestPreflightNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ORCA_CLI_COMMAND", "")
	t.Setenv("ORCA_DEV_REPO_ROOT", "")

	_, err := Preflight()
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("want ErrNotInstalled, got %v", err)
	}
	if !strings.Contains(err.Error(), "onorca.dev") {
		t.Errorf("message does not say how to install: %v", err)
	}
}

func TestPreflightNotResponding(t *testing.T) {
	fakeOrca(t, `echo "runtime down" >&2; exit 1`)

	_, err := Preflight()
	if !errors.Is(err, ErrNotResponding) {
		t.Fatalf("want ErrNotResponding, got %v", err)
	}
}

func TestPreflightUnreachableRuntime(t *testing.T) {
	fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":false}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
esac`)

	_, err := Preflight()
	if !errors.Is(err, ErrNotResponding) {
		t.Fatalf("want ErrNotResponding, got %v", err)
	}
}

func TestPreflightOpensThenSucceeds(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "opened")
	log := fakeOrca(t, fmt.Sprintf(`case "$1" in
  status)
    if [ -f %q ]; then
      echo '{"ok":true,"result":{"runtime":{"reachable":true}}}'
    else
      echo '{"ok":true,"result":{"runtime":{"reachable":false}}}'
    fi ;;
  open)
    touch %q
    echo '{"ok":true,"result":{}}' ;;
esac`, flag, flag))

	if _, err := Preflight(); err != nil {
		t.Fatalf("open then status should recover, got %v", err)
	}
	if !strings.Contains(readLog(t, log), "open --json") {
		t.Error("preflight did not try to open the app")
	}
}

func TestPreflightSucceedsWhenReady(t *testing.T) {
	fakeOrca(t, readyBody())

	if _, err := Preflight(); err != nil {
		t.Fatalf("a reachable runtime must be enough, got %v", err)
	}
}

const listJSON = `{"ok":true,"result":{"terminals":[
  {"handle":"term_4","title":"bbs foreman fm-a","connected":true,"worktreePath":"/repo"},
  {"handle":"term_2","title":"Grok","connected":true,"worktreePath":"/other"}
]}}`

func fakeWithTerminals(t *testing.T) (*Client, string) {
	t.Helper()
	log := fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
  repo) echo '{"ok":true,"result":{}}' ;;
  terminal)
    case "$2" in
      list) echo '`+listJSON+`' ;;
      create) echo '{"ok":true,"result":{"terminal":{"handle":"term_9","title":"bbs foreman fm-a"}}}' ;;
      read) echo '{"ok":true,"result":{"terminal":{"handle":"term_4","tail":["line one","line two"]}}}' ;;
      send|close) echo '{"ok":true,"result":{}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
  worktree) echo '{"ok":true,"result":{}}' ;;
  *) echo '{"ok":true,"result":{}}' ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	return c, log
}

func TestRefResolvesByTitle(t *testing.T) {
	c, _ := fakeWithTerminals(t)

	h, err := c.Ref("bbs foreman fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if h != "term_4" {
		t.Errorf("got handle %q, want term_4", h)
	}
}

func TestRefIgnoresUnrelatedTitle(t *testing.T) {
	c, _ := fakeWithTerminals(t)

	_, err := c.Ref("✳ Commit all changes")
	if !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("want ErrNoTerminal, got %v", err)
	}
}

func TestSendEnterUsesResolvedHandle(t *testing.T) {
	c, log := fakeWithTerminals(t)

	if err := c.SendEnter("bbs foreman fm-a", "check status"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "terminal send --terminal term_4 --text check status --enter --json") {
		t.Errorf("send did not resolve the title to term_4:\n%s", calls)
	}
}

func TestSetStatusMapsLaneAndUsesWorktree(t *testing.T) {
	c, log := fakeWithTerminals(t)

	if err := c.SetStatus("bbs foreman fm-a", "needs-attention"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "worktree set --worktree path:/repo --workspace-status in-review") {
		t.Errorf("status did not map the lane onto the worktree:\n%s", calls)
	}
}

func TestSetStatusRejectsUnknownLane(t *testing.T) {
	c, log := fakeWithTerminals(t)

	err := c.SetStatus("bbs foreman fm-a", "on-fire")
	if err == nil {
		t.Fatal("want an error for an unknown status")
	}
	if strings.Contains(readLog(t, log), "workspace-status") {
		t.Error("an invalid lane must not reach orca")
	}
}

func TestCapturePaneJoinsTail(t *testing.T) {
	c, _ := fakeWithTerminals(t)

	got, err := c.CapturePane("bbs foreman fm-a", 40)
	if err != nil {
		t.Fatal(err)
	}
	if got != "line one\nline two" {
		t.Errorf("got %q", got)
	}
}

func TestCloseMissingTerminalIsNotAnError(t *testing.T) {
	c, log := fakeWithTerminals(t)

	if err := c.Close("bbs foreman gone"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if strings.Contains(readLog(t, log), "terminal close") {
		t.Error("close must not run against an unresolved title")
	}
}

func TestRefReportsAFailedListing(t *testing.T) {
	fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true}}}' ;;
  terminal) echo "socket error" >&2; exit 1 ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.CapturePane("bbs foreman fm-a", 10); errors.Is(err, ErrNoTerminal) || err == nil {
		t.Fatalf("want the listing failure surfaced, got %v", err)
	}
	if err := c.Close("bbs foreman fm-a"); err == nil {
		t.Fatal("Close reported success without ever seeing the terminal list")
	}
}

func TestCreateReturnsHandleAndRegistersRepo(t *testing.T) {
	c, log := fakeWithTerminals(t)

	ref, err := c.Create(CreateOpts{Title: "bbs foreman fm-a", Cwd: "/repo", Command: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "term_9" {
		t.Errorf("want the handle from create, got %q", ref)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "repo add --path /repo --json") {
		t.Errorf("create did not register the repo:\n%s", calls)
	}
	if !strings.Contains(calls, "terminal create --title bbs foreman fm-a --worktree path:/repo --command claude --json") {
		t.Errorf("create did not pass title/cwd/command:\n%s", calls)
	}
}

func TestNotifyWritesWorktreeComment(t *testing.T) {
	c, log := fakeWithTerminals(t)

	if err := c.Notify("bbs foreman fm-a", "bs-x waiting", "approve the plan"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(t, log), "worktree set --worktree path:/repo --comment bs-x waiting — approve the plan") {
		t.Errorf("notify did not set the comment:\n%s", readLog(t, log))
	}
}

func TestEnvelopeErrorIsSurfaced(t *testing.T) {
	fakeOrca(t, `case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true}}}' ;;
  terminal) echo '{"ok":false,"error":{"message":"no such terminal"}}' ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CapturePane("bbs foreman fm-a", 10)
	if err == nil || !strings.Contains(err.Error(), "no such terminal") {
		t.Fatalf("want the envelope error, got %v", err)
	}
}

func TestHandleFromShapes(t *testing.T) {
	if got := handleFrom(json.RawMessage(`{"handle":"term_a"}`)); got != "term_a" {
		t.Errorf("bare handle: %q", got)
	}
	if got := handleFrom(json.RawMessage(`{"terminal":{"handle":"term_b"}}`)); got != "term_b" {
		t.Errorf("wrapped handle: %q", got)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
