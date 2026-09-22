package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/orca"
)

func resourceCLIFixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BABYSIT_HOME", home)
	t.Setenv("BABYSIT_STATE_DIR", home)
	// status reconciles leases against Orca; a missing binary is the "Orca
	// unreachable" path, which holds every lease — the default for tests that
	// are not about reconciliation.
	t.Setenv("ORCA_CLI_COMMAND", filepath.Join(home, "no-orca"))
	if err := foreman.Save(foreman.Record{ID: "fm-a", Heartbeat: foreman.Now()}); err != nil {
		t.Fatal(err)
	}
	old := newResourceBroker
	newResourceBroker = func() *foreman.ResourceBroker {
		return &foreman.ResourceBroker{
			Dir: filepath.Join(home, "resources"),
			Probe: func() foreman.HostResources {
				return foreman.HostResources{CPUs: 8, TotalMemoryBytes: 32 << 30}
			},
		}
	}
	t.Cleanup(func() { newResourceBroker = old })
}

func TestForemanResourceReserveStatusRelease(t *testing.T) {
	resourceCLIFixture(t)
	out := captureStdout(t, func() {
		if err := foremanResource([]string{
			"reserve", "fm-a", "--ticket", "bs-a", "--task", "task-a", "--profile", "standard",
		}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "ADMISSION=reserved\n") || !strings.Contains(out, "GLOBAL_BUDGET=8\n") {
		t.Fatalf("reserve output: %q", out)
	}
	var lease string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "LEASE=") {
			lease = strings.TrimPrefix(line, "LEASE=")
		}
	}
	if lease == "" {
		t.Fatalf("reserve omitted lease: %q", out)
	}

	out = captureStdout(t, func() {
		if err := foremanResource([]string{"status"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "GLOBAL_USED=2\n") || !strings.Contains(out, "ACTIVE_LEASE="+lease) {
		t.Fatalf("status output: %q", out)
	}

	out = captureStdout(t, func() {
		if err := foremanResource([]string{"release", lease}); err != nil {
			t.Fatal(err)
		}
	})
	if out != "RELEASED="+lease+"\n" {
		t.Fatalf("release output: %q", out)
	}
}

func TestForemanResourceRequiresRegisteredOwner(t *testing.T) {
	resourceCLIFixture(t)
	err := foremanResource([]string{
		"reserve", "fm-missing", "--ticket", "bs-a", "--task", "task-a", "--profile", "plan",
	})
	if err == nil || !strings.Contains(err.Error(), "foreman fm-missing") {
		t.Fatalf("error = %v", err)
	}
}

// The self-heal path: status is what every foreman wake runs, so it owns the
// lease cross-check. A proven-terminal Dispatch releases its lease; a live
// dispatch, a task with no dispatch record, and an unreachable Orca all hold.
func TestForemanResourceStatusReleasesTerminalLeases(t *testing.T) {
	resourceCLIFixture(t)
	home := os.Getenv("BABYSIT_HOME")
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// dispatch-show answers per task: task-done is terminal, task-live is
	// still dispatched, task-none has no dispatch record at all.
	stub := `#!/bin/sh
case "$1" in
  status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
  open) echo '{"ok":true,"result":{}}' ;;
  orchestration)
    case "$*" in
      *task-done*) echo '{"ok":true,"result":{"dispatch":{"id":"ctx_1","status":"completed"}}}' ;;
      *task-live*) echo '{"ok":true,"result":{"dispatch":{"id":"ctx_2","status":"dispatched"}}}' ;;
      *task-none*) echo '{"ok":true,"result":{"dispatch":null}}' ;;
      *) echo '{"ok":true,"result":{}}' ;;
    esac ;;
  *) echo '{"ok":true,"result":{}}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "orca"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCA_CLI_COMMAND", filepath.Join(bin, "orca"))

	reserve := func(task string) string {
		status, err := newResourceBroker().Reserve(foreman.ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: task, Profile: "standard"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return status.Lease.ID
	}
	doneLease := reserve("task-done")
	liveLease := reserve("task-live")
	noneLease := reserve("task-none")

	out := captureStdout(t, func() {
		if err := foremanResource([]string{"status"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "RELEASED_LEASE="+doneLease+"\n") {
		t.Fatalf("terminal dispatch lease not released: %q", out)
	}
	if strings.Contains(out, "ACTIVE_LEASE="+doneLease) {
		t.Fatalf("released lease still active: %q", out)
	}
	for _, lease := range []string{liveLease, noneLease} {
		if !strings.Contains(out, "ACTIVE_LEASE="+lease) {
			t.Fatalf("unproven lease %s was dropped: %q", lease, out)
		}
	}
	// GLOBAL_USED counts only the two held leases now.
	if !strings.Contains(out, "GLOBAL_USED=4\n") {
		t.Fatalf("status output: %q", out)
	}
}

func TestConfiguredResourceCapValidation(t *testing.T) {
	resourceCLIFixture(t)
	if got, err := configuredResourceCap(); err != nil || got != 0 {
		t.Fatalf("missing cap = %d, %v", got, err)
	}
	if err := config.Set("parallel_global_units", "3"); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredResourceCap(); err != nil || got != 3 {
		t.Fatalf("configured cap = %d, %v", got, err)
	}
	if err := config.Set("parallel_global_units", "many"); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredResourceCap(); err == nil {
		t.Fatal("invalid cap was accepted")
	}
}

func recoveryClient(t *testing.T) *orca.Client {
	t.Helper()
	home := os.Getenv("BABYSIT_HOME")
	stub := `#!/bin/sh
case "$1" in
 status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
 orchestration)
 case "$2" in
 dispatch-show)
  if [ -f "$BABYSIT_HOME/stopped" ] && [ "$STOP_RESULT" = settled ]; then
   echo '{"ok":true,"result":{"dispatch":{"id":"ctx-zombie","run_id":"run-old","status":"failed"}}}'
  else printf '%s\n' "$DISPATCH_RESPONSE"; fi ;;
 worker-list) printf '%s\n' "$FLEET_RESPONSE" ;;
 worker-stop)
  touch "$BABYSIT_HOME/stopped"
  if [ "$STOP_RESULT" = error ]; then echo '{"ok":false,"error":{"message":"stop failed"}}'; exit 1; fi
  echo '{"ok":true,"result":{}}' ;;
 esac ;;
esac
`
	path := filepath.Join(home, "orca-recovery")
	if err := os.WriteFile(path, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCA_CLI_COMMAND", path)
	c, err := orca.Preflight()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResourceRecoveryAfterOwnerInterruption(t *testing.T) {
	for _, tc := range []struct {
		name, owner, dispatch, verdict, stop string
		age                                  time.Duration
		released, stopped                    bool
	}{
		{name: "abandoned launch", owner: "stale", age: time.Hour, released: true},
		{name: "deleted owner", owner: "missing", age: time.Hour, released: true},
		{name: "live owner still launching", owner: "live", age: time.Hour},
		{name: "startup grace", owner: "stale", age: time.Minute},
		{name: "live worker outlives owner", owner: "stale", age: time.Hour, dispatch: "dispatched", verdict: "live"},
		{name: "lost contact is not exit", owner: "missing", age: time.Hour, dispatch: "dispatched", verdict: "unverifiable"},
		{name: "zombie agent in live terminal", owner: "stale", age: time.Hour, dispatch: "dispatched", verdict: "exited", stop: "settled", released: true, stopped: true},
		{name: "stuck worker with healthy owner", owner: "live", age: time.Hour, dispatch: "dispatched", verdict: "exited", stop: "settled", released: true, stopped: true},
		{name: "failed stop retains capacity", owner: "stale", age: time.Hour, dispatch: "dispatched", verdict: "exited", stop: "error", stopped: true},
		{name: "unconfirmed stop retains capacity", owner: "stale", age: time.Hour, dispatch: "dispatched", verdict: "exited", stop: "pending", stopped: true},
		{name: "completed worker", owner: "stale", age: time.Hour, dispatch: "completed", released: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resourceCLIFixture(t)
			now := time.Now().UTC()
			switch tc.owner {
			case "missing":
				if err := os.Remove(filepath.Join(foreman.Dir(), "fm-a.yaml")); err != nil {
					t.Fatal(err)
				}
			case "stale":
				if err := foreman.Save(foreman.Record{ID: "fm-a", Heartbeat: now.Add(-time.Hour).Format(time.RFC3339)}); err != nil {
					t.Fatal(err)
				}
			}
			response := `{"ok":true,"result":{"dispatch":null}}`
			if tc.dispatch != "" {
				response = fmt.Sprintf(`{"ok":true,"result":{"dispatch":{"id":"ctx-zombie","run_id":"run-old","status":%q}}}`, tc.dispatch)
			}
			t.Setenv("DISPATCH_RESPONSE", response)
			t.Setenv("FLEET_RESPONSE", fmt.Sprintf(`{"ok":true,"result":{"workers":[{"dispatchId":"ctx-zombie","projection":{"liveness":{"verdict":%q}}}],"page":{"hasMore":false}}}`, tc.verdict))
			t.Setenv("STOP_RESULT", tc.stop)
			c := recoveryClient(t)
			b := newResourceBroker()
			b.Now = func() time.Time { return now.Add(-tc.age) }
			seeded, err := b.Reserve(foreman.ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "task-zombie", Profile: "ios-simulator"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			released := reconcileResourceLeases(context.Background(), b, seeded.Leases, c, now)
			if (len(released) == 1) != tc.released {
				t.Fatalf("released=%v want %v", released, tc.released)
			}
			_, err = os.Stat(filepath.Join(os.Getenv("BABYSIT_HOME"), "stopped"))
			if (err == nil) != tc.stopped {
				t.Fatalf("worker-stop invoked=%v want %v", err == nil, tc.stopped)
			}
			status, err := b.Status(0)
			if err != nil {
				t.Fatal(err)
			}
			if (status.Used == 0) != tc.released {
				t.Fatalf("capacity=%d", status.Used)
			}
		})
	}
}

func TestReserveReclaimsOtherForemansCapacity(t *testing.T) {
	resourceCLIFixture(t)
	if err := foreman.Save(foreman.Record{ID: "fm-new", Heartbeat: foreman.Now()}); err != nil {
		t.Fatal(err)
	}
	b := newResourceBroker()
	if _, err := b.Reserve(foreman.ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "old", Profile: "ios-simulator"}, 0); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCH_RESPONSE", `{"ok":true,"result":{"dispatch":{"id":"old-done","status":"completed"}}}`)
	recoveryClient(t)
	out := captureStdout(t, func() {
		if err := foremanResourceReserve([]string{"fm-new", "--ticket", "bs-new", "--task", "new", "--profile", "ios-simulator"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "ADMISSION=reserved\n") || !strings.Contains(out, "RELEASED_LEASE=") {
		t.Fatalf("new owner blocked by completed worker: %s", out)
	}
	// The Task's previous terminal Dispatch must not release the pending retry.
	out = captureStdout(t, func() {
		if err := foremanResourceStatus(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "GLOBAL_USED=4\n") || strings.Contains(out, "RELEASED_LEASE=") {
		t.Fatalf("old completion consumed new reservation: %s", out)
	}
}

func TestResourceReserveEnforcesConfiguredWorkerLimit(t *testing.T) {
	resourceCLIFixture(t)
	if err := config.Set("parallel_max_workers", "1"); err != nil {
		t.Fatal(err)
	}
	reserve := func(task string) string {
		return captureStdout(t, func() {
			if err := foremanResourceReserve([]string{"fm-a", "--ticket", "bs-a", "--task", task, "--profile", "plan"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if out := reserve("one"); !strings.Contains(out, "ADMISSION=reserved\n") {
		t.Fatal(out)
	}
	if out := reserve("one"); !strings.Contains(out, "ADMISSION=reserved\n") {
		t.Fatalf("retry not idempotent: %s", out)
	}
	if out := reserve("two"); !strings.Contains(out, "ADMISSION=queued\n") {
		t.Fatal(out)
	}
}

func TestWatchRecoversCapacityWithoutOpenForeman(t *testing.T) {
	resourceCLIFixture(t)
	old := time.Now().Add(-time.Hour)
	if err := foreman.Save(foreman.Record{ID: "fm-a", Heartbeat: old.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	b := newResourceBroker()
	b.Now = func() time.Time { return old }
	if _, err := b.Reserve(foreman.ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "orphan", Profile: "standard"}, 0); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCH_RESPONSE", `{"ok":true,"result":{"dispatch":null}}`)
	recoveryClient(t)
	out := captureStdout(t, func() {
		if err := foremanWatch([]string{"--once"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "RELEASED_LEASE=") || !strings.Contains(out, "nothing to watch") {
		t.Fatalf("watch exited without recovering orphan: %s", out)
	}
}

func TestSlowLeaseCannotStarveOtherForemanRecovery(t *testing.T) {
	resourceCLIFixture(t)
	b := newResourceBroker()
	for _, task := range []string{"slow", "finished"} {
		if _, err := b.Reserve(foreman.ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: task, Profile: "plan"}, 0); err != nil {
			t.Fatal(err)
		}
	}
	status, err := b.Status(0)
	if err != nil {
		t.Fatal(err)
	}
	// Force the first lease to consume the whole tick's budget.
	t.Setenv("SLOW_TASK", status.Leases[0].Task)
	path := filepath.Join(os.Getenv("BABYSIT_HOME"), "orca-slow")
	stub := `#!/bin/sh
case "$1" in
 status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
 orchestration)
 if [ "$4" = "$SLOW_TASK" ]; then exec sleep 30; fi
 echo '{"ok":true,"result":{"dispatch":{"id":"done","status":"completed"}}}' ;;
esac
`
	if err := os.WriteFile(path, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCA_CLI_COMMAND", path)
	for tick := 0; tick < 2; tick++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := orca.PreflightContext(ctx)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		status, err := b.Status(0)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		released := reconcileResourceLeases(ctx, b, status.Leases, c, time.Now())
		cancel()
		if tick == 1 && len(released) != 1 {
			t.Fatalf("slow lease starved recovery again: %v", released)
		}
	}
}
