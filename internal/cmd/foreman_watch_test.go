package cmd

import (
	"fmt"
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
	// statusInterval sits past every horizon the legacy tests exercise, so
	// they keep testing the stall clock alone; the status tests set their own.
	return watchOpts{interval: time.Minute, idle: 10 * time.Minute,
		statusInterval: time.Hour, lines: 40, nudge: "check status", maxNudges: 2}
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
	if !strings.Contains(callLog(t, log), "terminal send --terminal term_0 --text /bbs:foreman --foreman-id fm-test check status --enter") {
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
	if !strings.Contains(calls, "terminal send --terminal term_0 --text /bbs:foreman --foreman-id fm-test check status --enter") {
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

// The status clock runs on the calendar, not the pane: a foreman whose output
// never stops moving still gets asked for status once the interval elapses.
func TestWatchStatusCheckOnMovingPane(t *testing.T) {
	client, r, pane, log := watchFixture(t)
	o := testWatchOpts()
	o.statusInterval = 15 * time.Minute
	now := time.Now()

	watchTick(client, r, o, now)
	write(t, pane, "worker A: still building\n")
	line := watchTick(client, r, o, now.Add(16*time.Minute))
	if !strings.HasPrefix(line, "STATUS fm-test") {
		t.Fatalf("a moving pane must still get the status check, got %q", line)
	}
	if !strings.Contains(callLog(t, log), "terminal send --terminal term_0 --text /bbs:foreman --foreman-id fm-test check status --enter") {
		t.Error("expected the status prompt to be sent")
	}
	if s := watchLoad(r.ID); s.Nudges != 0 || !s.PendingStatus {
		t.Fatalf("status check must not spend the nudge budget: %+v", s)
	}
}

// The interval is a floor, not a suggestion: a tick inside the window sends
// nothing, so a fast --interval cannot multiply the prompts.
func TestWatchNoEarlyStatusCheck(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	o.statusInterval = 15 * time.Minute
	now := time.Now()

	watchTick(client, r, o, now)
	write(t, pane, "worker A: still building\n")
	if line := watchTick(client, r, o, now.Add(14*time.Minute)); line != "" {
		t.Fatalf("status check fired early: %q", line)
	}
	if s := watchLoad(r.ID); s.PendingStatus {
		t.Fatal("status prompt sent before the interval elapsed")
	}
}

// A status prompt is a check, not the stall response: it must neither spend
// the nudge budget nor restart the idle window, so a dead terminal reaches
// STALLED on the same bound it would without the loop.
func TestWatchStatusChecksDoNotEvadeStall(t *testing.T) {
	client, r, _, _ := watchFixture(t)
	o := testWatchOpts()
	o.statusInterval = 5 * time.Minute
	now := time.Now()

	watchTick(client, r, o, now)
	// Status check lands mid-idle; the pane never echoes it back.
	if line := watchTick(client, r, o, now.Add(5*time.Minute)); !strings.HasPrefix(line, "STATUS") {
		t.Fatalf("expected STATUS, got %q", line)
	}
	// The nudge still fires on the original idle window, not 10m after the
	// status prompt.
	line := watchTick(client, r, o, now.Add(10*time.Minute))
	if !strings.HasPrefix(line, "NUDGED fm-test after 10m (1/2)") {
		t.Fatalf("status prompt deferred the nudge: got %q", line)
	}
	// Second nudge coalesces with the next due status check: one prompt, and
	// the nudge budget is still spent.
	line = watchTick(client, r, o, now.Add(20*time.Minute))
	if !strings.HasPrefix(line, "NUDGED fm-test after 10m (2/2)") {
		t.Fatalf("expected coalesced nudge, got %q", line)
	}
	if s := watchLoad(r.ID); s.StatusCheck == "" {
		t.Fatal("coalesced send must stamp the status clock")
	}
	line = watchTick(client, r, o, now.Add(30*time.Minute))
	if !strings.HasPrefix(line, "STALLED fm-test — 2 nudges") {
		t.Fatalf("expected STALLED, got %q", line)
	}
	// Declared stalled: the status loop stops poking too.
	if line := watchTick(client, r, o, now.Add(40*time.Minute)); line != "" {
		t.Fatalf("a stalled foreman must go quiet, got %q", line)
	}
}

// The pathological configuration: --status-interval <= --interval on a
// terminal that echoes every prompt but never answers one. Each tick is a
// STATUS-ECHO followed by another send, so the pane never sits unchanged —
// if the stall verdict required an unmoved tick it would be starved forever.
// The loop must still reach STALLED in a bounded number of ticks.
func TestWatchStatusFloodStillStalls(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	o.interval = time.Minute
	o.statusInterval = time.Minute // <= poll interval: a prompt is due every tick
	now := time.Now()

	watchTick(client, r, o, now)
	stalled := ""
	// The bound is generous — nudges alone need ~maxNudges*idle — but finite:
	// a loop that never stalls fails here by exhausting the tick budget.
	for i := 1; i <= 200 && stalled == ""; i++ {
		tick := now.Add(time.Duration(i) * time.Minute)
		if line := watchTick(client, r, o, tick); strings.HasPrefix(line, "STALLED") {
			stalled = line
			break
		}
		// The dead terminal echoes whatever was just sent into its pane.
		write(t, pane, fmt.Sprintf("worker A: building\n> check status x%d\n", i))
	}
	if stalled == "" {
		t.Fatal("status flood starved the stall verdict: no STALLED in 200 ticks")
	}
	// Declared stalled: even with a prompt due every tick, the loop goes quiet.
	if line := watchTick(client, r, o, now.Add(300*time.Minute)); line != "" {
		t.Fatalf("a stalled foreman must go quiet, got %q", line)
	}
}

// A status echo that lands after the nudge it outlived is still an echo:
// two prompts in flight means two claims, or the second echo reads as
// progress and refunds the budget it just spent.
func TestWatchLaggingStatusEchoDoesNotRefund(t *testing.T) {
	client, r, pane, _ := watchFixture(t)
	o := testWatchOpts()
	o.statusInterval = 5 * time.Minute
	now := time.Now()

	watchTick(client, r, o, now)
	// Status prompt at t=5m; its echo has not landed yet when the nudge
	// fires at t=10m — two prompts in flight.
	if line := watchTick(client, r, o, now.Add(5*time.Minute)); !strings.HasPrefix(line, "STATUS") {
		t.Fatalf("expected STATUS, got %q", line)
	}
	if line := watchTick(client, r, o, now.Add(10*time.Minute)); !strings.HasPrefix(line, "NUDGED") {
		t.Fatalf("expected NUDGED, got %q", line)
	}
	// The status echo arrives late, then the nudge echo: both are echoes.
	write(t, pane, "worker A: building\n> check status\n")
	watchTick(client, r, o, now.Add(11*time.Minute))
	write(t, pane, "worker A: building\n> check status\n> check status\n")
	watchTick(client, r, o, now.Add(12*time.Minute))
	if s := watchLoad(r.ID); s.Nudges != 1 {
		t.Fatalf("lagging status echo refunded the budget: nudges = %d, want 1", s.Nudges)
	}
}

// A foreman that reported itself done is out of the watch set even while Orca
// still has its terminal — the batch closed, the pane is a leftover.
func TestWatchTargetsSkipDoneForemen(t *testing.T) {
	client, r, _, _ := watchFixture(t)
	r.Status = "done"
	if err := foreman.Save(r); err != nil {
		t.Fatal(err)
	}
	targets, err := watchTargets(client, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("done foreman stayed in the watch set: %+v", targets)
	}
	// Only the canonical status is terminal; anything else stays watchable.
	r.Status = "wrapping up"
	if err := foreman.Save(r); err != nil {
		t.Fatal(err)
	}
	targets, err = watchTargets(client, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "fm-test" {
		t.Fatalf("non-done status dropped from the watch set: %+v", targets)
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
	// watchOptsFrom reads foreman_status_interval from the global config, so
	// isolate it: no file means the documented 3600-second default.
	t.Setenv("BABYSIT_STATE_DIR", t.TempDir())
	o, err := watchOptsFrom(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if o.idle != 10*time.Minute || o.interval != time.Minute || o.statusInterval != time.Hour || o.nudge != "check status" || o.maxNudges != 3 {
		t.Errorf("unexpected defaults: %+v", o)
	}
	o, err = watchOptsFrom(map[string]string{"idle": "90", "status-interval": "45", "nudge": "status?", "once": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if o.idle != 90*time.Second || o.statusInterval != 45*time.Second || o.nudge != "status?" || !o.once {
		t.Errorf("flags not applied: %+v", o)
	}
	for _, bad := range []map[string]string{
		{"idle": "0"}, {"idle": "soon"}, {"status-interval": "0"}, {"lines": "-1"}, {"max-nudges": "-1"}, {"nudge": "  "},
	} {
		if _, err := watchOptsFrom(bad); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

// The status clock's default is the configured reconciliation interval, and
// the explicit flag still wins over it — the same precedence the Foreman
// skill documents for its check --wait bound.
func TestWatchStatusIntervalFromConfig(t *testing.T) {
	state := t.TempDir()
	t.Setenv("BABYSIT_STATE_DIR", state)
	cfg := filepath.Join(state, "config.yaml")

	write(t, cfg, "foreman_status_interval: 1800\n")
	o, err := watchOptsFrom(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if o.statusInterval != 30*time.Minute {
		t.Errorf("configured value not applied: %+v", o)
	}

	o, err = watchOptsFrom(map[string]string{"status-interval": "120"})
	if err != nil {
		t.Fatal(err)
	}
	if o.statusInterval != 2*time.Minute {
		t.Errorf("explicit --status-interval must win over config: %+v", o)
	}

	// A present-but-invalid value fails loudly — never a silent tight loop.
	for _, v := range []string{"0", "-5", "soon", "99999999999999999999"} {
		write(t, cfg, "foreman_status_interval: "+v+"\n")
		if _, err := watchOptsFrom(map[string]string{}); err == nil {
			t.Errorf("expected an error for foreman_status_interval=%q", v)
		}
		// The explicit flag rescues the run: it wins before config is read.
		if _, err := watchOptsFrom(map[string]string{"status-interval": "60"}); err != nil {
			t.Errorf("explicit flag should bypass invalid config %q: %v", v, err)
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
