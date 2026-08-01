package cmux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCmux puts a stub `cmux` on PATH whose behavior is the given script body,
// dispatched on "$1", and returns its call log path. Preflight shells out, so
// a stub binary is the only way to exercise it without a running cmux.
func fakeCmux(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CMUX_SOCKET_CAPABILITY", "test-token")
	return log
}

func TestPreflightNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CMUX_SOCKET_CAPABILITY", "test-token")

	_, err := Preflight()
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("want ErrNotInstalled, got %v", err)
	}
}

// A denial with no token in the environment is the missing-token case: cmux
// is installed and running, and still refuses every call.
func TestPreflightNoCapability(t *testing.T) {
	fakeCmux(t, `echo "ERROR: Access denied - only processes started inside cmux can connect" >&2; exit 1`)
	t.Setenv("CMUX_SOCKET_CAPABILITY", "")

	_, err := Preflight()
	if !errors.Is(err, ErrNoCapability) {
		t.Fatalf("want ErrNoCapability, got %v", err)
	}
	// The whole point of a distinct error here is that cmux is installed and
	// running: the message has to carry the fix, not just the symptom.
	if !strings.Contains(err.Error(), "CMUX_SOCKET_CAPABILITY=") {
		t.Errorf("message does not say how to fix it: %v", err)
	}
}

// Same denial, but a token WAS supplied — so the token is not the explanation.
func TestPreflightNotResponding(t *testing.T) {
	fakeCmux(t, `echo "socket denied" >&2; exit 1`)

	_, err := Preflight()
	if !errors.Is(err, ErrNotResponding) {
		t.Fatalf("want ErrNotResponding, got %v", err)
	}
}

// A socket that answers but does not say PONG is as unusable as one that
// refuses — an exit-0 non-PONG must not produce a working client.
func TestPreflightWrongPong(t *testing.T) {
	fakeCmux(t, `echo nope`)

	if _, err := Preflight(); !errors.Is(err, ErrNotResponding) {
		t.Fatalf("want ErrNotResponding, got %v", err)
	}
}

// Ancestry is the other way in: a cmux descendant is served with no CMUX_*
// environment at all, so an unset token must NOT be a precondition.
func TestPreflightSucceedsWithoutTokenWhenSocketAnswers(t *testing.T) {
	fakeCmux(t, `echo PONG`)
	t.Setenv("CMUX_SOCKET_CAPABILITY", "")

	if _, err := Preflight(); err != nil {
		t.Fatalf("a responding socket must be enough, got %v", err)
	}
}

const windowsJSON = `[{"id":"9090E003-4A69-43A3-8233-02A3B987AF7A","index":0},
                      {"id":"1111AAAA-0000-0000-0000-000000000000","index":1}]`

// listWindows text form is `* 0: <UUID> selected_workspace=<UUID> workspaces=3`
// — the leading "* " on the selected row is why this parses --json instead.
func TestListWindowsParsesFields(t *testing.T) {
	fakeCmux(t, `case "$1" in
  ping) echo PONG ;;
  list-windows) echo '`+windowsJSON+`' ;;
esac`)

	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	ws, err := c.ListWindows()
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("want 2 windows, got %d", len(ws))
	}
	if ws[0].ID != "9090E003-4A69-43A3-8233-02A3B987AF7A" || ws[0].Index != 0 {
		t.Errorf("window 0 mis-parsed: %+v", ws[0])
	}
	if ws[1].Index != 1 {
		t.Errorf("window 1 mis-parsed: %+v", ws[1])
	}
}

// One window's workspaces: a custom-titled one (a real handle) and one whose
// title is agent-generated (must never match).
const spacesJSON = `{"window_ref":"window:1","workspaces":[
  {"ref":"workspace:4","custom_title":"bbs foreman fm-a","has_custom_title":true,
   "title":"bbs foreman fm-a","index":0,"current_directory":"/repo"},
  {"ref":"workspace:2","custom_title":null,"has_custom_title":false,
   "title":"✳ Commit all changes","index":1,"current_directory":"/other"}]}`

func fakeWithWorkspaces(t *testing.T) (*Client, string) {
	t.Helper()
	log := fakeCmux(t, `case "$1" in
  ping) echo PONG ;;
  list-windows) echo '`+windowsJSON+`' ;;
  workspace)
    case "$2" in
      list) echo '`+spacesJSON+`' ;;
      *) echo OK ;;
    esac ;;
  *) echo OK ;;
esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	return c, log
}

func TestWorkspaceRefResolvesByCustomTitle(t *testing.T) {
	c, log := fakeWithWorkspaces(t)

	if err := c.Send("bbs foreman fm-a", "hello"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "send --workspace workspace:4 -- hello") {
		t.Errorf("send did not resolve the title to workspace:4:\n%s", calls)
	}
}

// The generated title is not a handle — matching it would send into whichever
// workspace happens to be showing that activity label.
func TestWorkspaceRefIgnoresGeneratedTitle(t *testing.T) {
	c, _ := fakeWithWorkspaces(t)

	err := c.Send("✳ Commit all changes", "hello")
	if !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace, got %v", err)
	}
}

func TestSendKeyAndStatusUseResolvedRef(t *testing.T) {
	c, log := fakeWithWorkspaces(t)

	if err := c.SendKey("bbs foreman fm-a", "ctrl+u"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetStatus("bbs foreman fm-a", "working"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "send-key --workspace workspace:4 ctrl+u") {
		t.Errorf("send-key did not use the resolved ref:\n%s", calls)
	}
	if !strings.Contains(calls, "workspace status set working --workspace workspace:4") {
		t.Errorf("status set did not use the resolved ref:\n%s", calls)
	}
}

// cmux rejects any lane outside the five (plus auto) with invalid_params;
// catching it here keeps the error actionable instead of a socket rejection.
func TestSetStatusRejectsUnknownLane(t *testing.T) {
	c, log := fakeWithWorkspaces(t)

	err := c.SetStatus("bbs foreman fm-a", "in_progress")
	if err == nil {
		t.Fatal("want an error for a non-lane status")
	}
	if !strings.Contains(err.Error(), "needs-attention") {
		t.Errorf("error should list the accepted lanes: %v", err)
	}
	if strings.Contains(readLog(t, log), "status set") {
		t.Error("an invalid lane must not reach cmux")
	}
}

// Closing something already gone is the outcome the caller wanted.
func TestCloseMissingWorkspaceIsNotAnError(t *testing.T) {
	c, log := fakeWithWorkspaces(t)

	if err := c.Close("bbs foreman gone"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if strings.Contains(readLog(t, log), "workspace close") {
		t.Error("close must not run against an unresolved title")
	}
}

// Create has no caller window when the server is detached, so it must name one
// itself or cmux fails with no window context.
func TestCreatePassesAWindow(t *testing.T) {
	c, log := fakeWithWorkspaces(t)

	ref, err := c.Create(CreateOpts{Title: "bbs foreman fm-a", Cwd: "/repo", Command: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "workspace:4" {
		t.Errorf("want the ref re-derived from the title, got %q", ref)
	}
	if !strings.Contains(readLog(t, log), "--window 9090E003-4A69-43A3-8233-02A3B987AF7A") {
		t.Errorf("create did not name a window:\n%s", readLog(t, log))
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
