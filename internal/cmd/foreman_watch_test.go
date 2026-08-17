package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/orca"
)

// watchFixture stands up a stub orca whose pane content is a file the test
// rewrites, plus a registered foreman pointing at it. The pane file IS the
// control surface: writing the same bytes twice is a foreman that stopped
// working, which is the only condition this command reacts to.
//
// It returns the client, the record, the pane file and the call log.
func watchFixture(t *testing.T) (*orca.Client, foreman.Record, string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	pane := filepath.Join(dir, "pane.txt")
	titles := filepath.Join(dir, "titles")
	write(t, titles, "bbs foreman\n")
	write(t, pane, "worker A: building\n")

	// PATH inside the stub is its own: the Go process runs with PATH=dir so it
	// can only find this orca, which leaves `python3` unresolvable unless we
	// restore a real PATH for the script body.
	script := `#!/bin/sh
PATH=/bin:/usr/bin
printf '%s\n' "$*" >> ` + log + `
titles=` + titles + `
pane=` + pane + `
case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
  terminal)
    case "$2" in
      list)
        python3 -c '
import json
titles=open("'"$titles"'").read().splitlines()
terms=[{"handle":"term_%d"%i,"title":t,"connected":True,"worktreePath":"/repo"}
       for i,t in enumerate(titles) if t]
print(json.dumps({"ok":True,"result":{"terminals":terms}}))
' ;;
      read)
        python3 -c '
import json
lines=open("'"$pane"'").read().splitlines()
print(json.dumps({"ok":True,"result":{"terminal":{"handle":"term_0","tail":lines}}}))
' ;;
      send|close) echo '{"ok":true,"result":{}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
  worktree) echo '{"ok":true,"result":{}}' ;;
  *) echo '{"ok":true,"result":{}}' ;;
esac
`
	write(t, filepath.Join(dir, "orca"), script)
	if err := os.Chmod(filepath.Join(dir, "orca"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ORCA_CLI_COMMAND", "")
	t.Setenv("ORCA_DEV_REPO_ROOT", "")
	t.Setenv("BABYSIT_HOME", t.TempDir())

	client, err := orca.Preflight()
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	r := foreman.Record{ID: "fm-test", WorkspaceTitle: "bbs foreman", Heartbeat: foreman.Now()}
	if err := foreman.Save(r); err != nil {
		t.Fatal(err)
	}
	return client, r, pane, log
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testWatchOpts() watchOpts {
	return watchOpts{interval: time.Minute, idle: 10 * time.Minute, lines: 40,
		nudge: "check status", maxNudges: 2}
}

// A pane that keeps changing is a foreman that is working. It must produce no
// output at all in loop mode — a watchdog that narrates every tick buries the
// one line that matters.
func TestWatchQuietWhileMoving(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	now := time.Now()

	if line := watchTick(client, r, o, now); line != "" {
		t.Fatalf("first sighting should be quiet, got %q", line)
	}
	// Well past the idle window, but the pane moved — no nudge.
	write(t, pane, "worker A: running tests\n")
	if line := watchTick(client, r, o, now.Add(30*time.Minute)); line != "" {
		t.Fatalf("a moving pane must stay quiet, got %q", line)
	}
	if s := watchLoad(r.ID); s.Nudges != 0 {
		t.Errorf("nudges = %d, want 0", s.Nudges)
	}
}

// Terminal padding is not content: the same output re-rendered at a different
// window width must not read as progress and reset the idle clock.
func TestWatchIgnoresTrailingWhitespace(t *testing.T) {
	client, r, pane, log := watchFixture(t)
	o := testWatchOpts()
	now := time.Now()

	watchTick(client, r, o, now)
	write(t, pane, "worker A: building   \n")
	line := watchTick(client, r, o, now.Add(11*time.Minute))
	if !strings.HasPrefix(line, "NUDGED") {
		t.Fatalf("re-padded pane should still be idle, got %q", line)
	}
	if !strings.Contains(callLog(t, log), "terminal send --terminal term_0 --text check status --enter") {
		t.Error("expected the nudge text to be sent")
	}
}

// The core loop: unchanged pane past --idle → one nudge, delivered as text plus
// a real Enter. Text with no keypress behind it sits in the composer unsent.
func TestWatchNudgesAfterIdle(t *testing.T) {
	client, r, _, log := watchFixture(t)
	o := testWatchOpts()
	now := time.Now()

	watchTick(client, r, o, now)
	if line := watchTick(client, r, o, now.Add(9*time.Minute)); line != "" {
		t.Fatalf("must not nudge before --idle elapses, got %q", line)
	}
	line := watchTick(client, r, o, now.Add(10*time.Minute))
	if !strings.HasPrefix(line, "NUDGED fm-test after 10m (1/2)") {
		t.Fatalf("got %q", line)
	}
	calls := callLog(t, log)
	if !strings.Contains(calls, "terminal send --terminal term_0 --text check status --enter") {
		t.Errorf("nudge text not sent and submitted; calls:\n%s", calls)
	}
}

// The nudge changes the pane by arriving in it. If that echo counted as
// progress the budget would reset every time and --max-nudges would never
// bind — the watchdog would poke a dead session forever.
func TestWatchEchoDoesNotRefundTheBudget(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	now := time.Now()

	watchTick(client, r, o, now)
	now = now.Add(10 * time.Minute)
	if line := watchTick(client, r, o, now); !strings.Contains(line, "(1/2)") {
		t.Fatalf("first nudge: got %q", line)
	}
	// The nudge lands in the pane, and nothing else happens after it.
	write(t, pane, "worker A: building\n> check status\n")
	if line := watchTick(client, r, o, now.Add(time.Minute)); line != "" {
		t.Fatalf("echo tick should be quiet, got %q", line)
	}
	if s := watchLoad(r.ID); s.Nudges != 1 {
		t.Fatalf("echo refunded the budget: nudges = %d, want 1", s.Nudges)
	}

	now = now.Add(12 * time.Minute)
	if line := watchTick(client, r, o, now); !strings.Contains(line, "(2/2)") {
		t.Fatalf("second nudge: got %q", line)
	}
	write(t, pane, "worker A: building\n> check status\n> check status\n")
	watchTick(client, r, o, now.Add(time.Minute))

	// Budget spent: say so once, then go quiet rather than poking forever.
	line := watchTick(client, r, o, now.Add(20*time.Minute))
	if !strings.HasPrefix(line, "STALLED fm-test — 2 nudges") {
		t.Fatalf("expected STALLED, got %q", line)
	}
	if again := watchTick(client, r, o, now.Add(40*time.Minute)); again != "" {
		t.Fatalf("STALLED must be reported once, got %q", again)
	}
}

// Two ticks of independent progress clear the budget: the first is written off
// as the nudge's own echo, the second cannot be.
func TestWatchRealProgressClearsTheBudget(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	now := time.Now()

	watchTick(client, r, o, now)
	watchTick(client, r, o, now.Add(10*time.Minute))
	write(t, pane, "worker A: building\n> check status\n")
	watchTick(client, r, o, now.Add(11*time.Minute))
	write(t, pane, "worker A: QA passed, handing off\n")
	watchTick(client, r, o, now.Add(12*time.Minute))

	if s := watchLoad(r.ID); s.Nudges != 0 || s.Pending {
		t.Fatalf("real progress must clear the budget: %+v", s)
	}
}

// A closed workspace is the batch finishing, not a failure — and must not be
// reported as unreachable, which is what a human would go investigate.
func TestWatchReportsAClosedWorkspace(t *testing.T) {
	client, r, _, _ := watchFixture(t)
	r.WorkspaceTitle = "bbs nobody"
	line := watchTick(client, r, testWatchOpts(), time.Now())
	if !strings.HasPrefix(line, "GONE") {
		t.Fatalf("got %q", line)
	}
}

// Selection is by open workspace, not by Live(): a foreman wedged long enough
// to need a nudge is exactly the one whose heartbeat has gone stale, since it
// writes that heartbeat itself.
func TestWatchTargetsIncludeStaleForemen(t *testing.T) {
	client, r, _, _ := watchFixture(t)
	r.Heartbeat = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if err := foreman.Save(r); err != nil {
		t.Fatal(err)
	}
	if r.Live() {
		t.Fatal("fixture should be stale")
	}
	targets, err := watchTargets(client, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "fm-test" {
		t.Fatalf("stale foreman dropped from the watch set: %+v", targets)
	}
}

// A record whose terminal Orca no longer has is not watchable, so a bare watch
// must not report it every interval.
func TestWatchTargetsSkipClosedWorkspaces(t *testing.T) {
	client, r, _, _ := watchFixture(t)
	r.ID, r.WorkspaceTitle = "fm-closed", "bbs gone"
	if err := foreman.Save(r); err != nil {
		t.Fatal(err)
	}
	targets, err := watchTargets(client, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "fm-test" {
		t.Fatalf("want only the open workspace, got %+v", targets)
	}
}

func TestWatchOptsDefaultsAndValidation(t *testing.T) {
	o, err := watchOptsFrom(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if o.idle != 10*time.Minute || o.interval != time.Minute || o.nudge != "check status" || o.maxNudges != 3 {
		t.Errorf("unexpected defaults: %+v", o)
	}
	o, err = watchOptsFrom(map[string]string{"idle": "90", "nudge": "status?", "once": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if o.idle != 90*time.Second || o.nudge != "status?" || !o.once {
		t.Errorf("flags not applied: %+v", o)
	}
	for _, bad := range []map[string]string{
		{"idle": "0"}, {"idle": "soon"}, {"lines": "-1"}, {"max-nudges": "-1"}, {"nudge": "  "},
	} {
		if _, err := watchOptsFrom(bad); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

func callLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
