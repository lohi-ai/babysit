package foreman

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func testResourceBroker(t *testing.T, host HostResources) *ResourceBroker {
	t.Helper()
	return &ResourceBroker{
		Dir:   t.TempDir(),
		Probe: func() HostResources { return host },
		Now:   func() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) },
	}
}

func healthyHost(cpus, memoryGiB int) HostResources {
	total := uint64(memoryGiB) * gib
	return HostResources{
		CPUs:                 cpus,
		TotalMemoryBytes:     total,
		AvailableMemoryBytes: total * 3 / 4,
		Load1:                0.5,
		MemoryKnown:          true,
		LoadKnown:            true,
	}
}

func TestResourceBudgetUsesCPUAndRAM(t *testing.T) {
	if got := resourceBudget(healthyHost(8, 16), 0); got != 8 {
		t.Fatalf("8 CPU / 16 GiB budget = %d, want 8", got)
	}
	if got := resourceBudget(healthyHost(16, 64), 3); got != 3 {
		t.Fatalf("configured ceiling budget = %d, want 3", got)
	}
	if got := resourceBudget(healthyHost(2, 8), 0); got != 2 {
		t.Fatalf("small host budget = %d, want 2", got)
	}
}

func TestResourceReservationsAreGlobalAcrossForemen(t *testing.T) {
	broker := testResourceBroker(t, healthyHost(8, 16)) // eight global units
	requests := []ResourceRequest{
		{ForemanID: "fm-a", Ticket: "bs-a", Task: "task-a", Profile: "standard"},
		{ForemanID: "fm-b", Ticket: "bs-b", Task: "task-b", Profile: "standard"},
		{ForemanID: "fm-c", Ticket: "bs-c", Task: "task-c", Profile: "standard"},
		{ForemanID: "fm-d", Ticket: "bs-d", Task: "task-d", Profile: "standard"},
		{ForemanID: "fm-e", Ticket: "bs-e", Task: "task-e", Profile: "standard"},
	}

	results := make([]ResourceStatus, len(requests))
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = broker.Reserve(requests[i], 0)
		}(i)
	}
	wg.Wait()

	reserved, queued := 0, 0
	for i, result := range results {
		if errs[i] != nil {
			t.Fatalf("reserve %d: %v", i, errs[i])
		}
		switch result.Admission {
		case "reserved":
			reserved++
		case "queued":
			queued++
		default:
			t.Fatalf("reserve %d admission = %q", i, result.Admission)
		}
	}
	if reserved != 4 || queued != 1 {
		t.Fatalf("reserved=%d queued=%d, want 4/1", reserved, queued)
	}
	status, err := broker.Status(0)
	if err != nil {
		t.Fatal(err)
	}
	if status.Used != 8 || len(status.Leases) != 4 {
		t.Fatalf("used=%d leases=%d, want 8/4", status.Used, len(status.Leases))
	}
}

func TestHeavyResourcesAreExclusiveAcrossForemen(t *testing.T) {
	broker := testResourceBroker(t, healthyHost(16, 64)) // sixteen global units
	first, err := broker.Reserve(ResourceRequest{
		ForemanID: "fm-a", Ticket: "bs-a", Task: "android", Profile: "android-simulator",
	}, 0)
	if err != nil || first.Admission != "reserved" {
		t.Fatalf("first reserve: admission=%q err=%v", first.Admission, err)
	}
	for _, req := range []ResourceRequest{
		{ForemanID: "fm-b", Ticket: "bs-b", Task: "ios", Profile: "ios-simulator"},
		{ForemanID: "fm-c", Ticket: "bs-c", Task: "ml", Profile: "local-ml"},
	} {
		got, err := broker.Reserve(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Admission != "queued" || got.Reason == "" {
			t.Fatalf("%s admission=%q reason=%q, want queued conflict", req.Profile, got.Admission, got.Reason)
		}
	}
}

func TestHostPressureQueuesWithoutMutatingLeases(t *testing.T) {
	host := healthyHost(8, 16)
	host.Load1 = 7
	broker := testResourceBroker(t, host)
	got, err := broker.Reserve(ResourceRequest{
		ForemanID: "fm-a", Ticket: "bs-a", Task: "plan", Profile: "plan",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Admission != "queued" || got.Reason == "" {
		t.Fatalf("admission=%q reason=%q", got.Admission, got.Reason)
	}
	status, err := broker.Status(0)
	if err != nil {
		t.Fatal(err)
	}
	if status.Used != 0 || len(status.Leases) != 0 {
		t.Fatalf("pressure queue mutated state: %+v", status)
	}
}

func TestReservationRetryAndReleaseAreIdempotent(t *testing.T) {
	broker := testResourceBroker(t, healthyHost(8, 16))
	req := ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "task-a", Profile: "standard"}
	first, err := broker.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := broker.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Lease == nil || second.Lease == nil || first.Lease.ID != second.Lease.ID {
		t.Fatalf("retry minted a second lease: first=%+v second=%+v", first.Lease, second.Lease)
	}
	if len(second.Leases) != 1 {
		t.Fatalf("retry left %d leases, want 1", len(second.Leases))
	}
	released, err := broker.Release(first.Lease.ID)
	if err != nil || !released {
		t.Fatalf("release=%v err=%v", released, err)
	}
	released, err = broker.Release(first.Lease.ID)
	if err != nil || released {
		t.Fatalf("second release=%v err=%v, want idempotent no-op", released, err)
	}
}

func TestResourceWorkerLimitIsAtomicAndPerForeman(t *testing.T) {
	broker := testResourceBroker(t, healthyHost(32, 64))
	var wg sync.WaitGroup
	results := make(chan ResourceStatus, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := broker.Reserve(ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: fmt.Sprint(i), Profile: "plan", MaxWorkers: 2}, 0)
			if err != nil {
				t.Error(err)
				return
			}
			results <- got
		}(i)
	}
	wg.Wait()
	close(results)
	admitted := 0
	for got := range results {
		if got.Admission == "reserved" {
			admitted++
		}
	}
	if admitted != 2 {
		t.Fatalf("admitted %d workers, want 2", admitted)
	}
	other, err := broker.Reserve(ResourceRequest{ForemanID: "fm-b", Ticket: "bs-b", Task: "other", Profile: "plan", MaxWorkers: 2}, 0)
	if err != nil || other.Admission != "reserved" {
		t.Fatalf("other foreman blocked: %+v %v", other, err)
	}
}

func TestDelayedReleaseCannotRemoveReplacement(t *testing.T) {
	broker := testResourceBroker(t, healthyHost(8, 16))
	req := ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "retry", Profile: "plan"}
	first, err := broker.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Release(first.Lease.ID); err != nil {
		t.Fatal(err)
	}
	second, err := broker.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Lease.ID == second.Lease.ID {
		t.Fatal("replacement reused the old lease generation")
	}
	if released, err := broker.Release(first.Lease.ID); err != nil || released {
		t.Fatalf("old release = %v, %v", released, err)
	}
	status, err := broker.Status(0)
	if err != nil || len(status.Leases) != 1 || status.Leases[0].ID != second.Lease.ID {
		t.Fatalf("replacement lost: %+v %v", status, err)
	}
}

func TestRecoveryCannotReleaseRenewedLaunch(t *testing.T) {
	b := testResourceBroker(t, healthyHost(8, 16))
	req := ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: "launch", Profile: "plan"}
	first, err := b.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := b.Reserve(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if released, err := b.ReleaseObserved(*first.Lease); err != nil || released {
		t.Fatalf("stale recovery removed renewed launch: %v %v", released, err)
	}
	if released, err := b.ReleaseObserved(*retry.Lease); err != nil || !released {
		t.Fatalf("current observation cannot release: %v %v", released, err)
	}
}

func TestReconciliationCursorSurvivesRestartAndRemoval(t *testing.T) {
	b := testResourceBroker(t, healthyHost(8, 16))
	for _, task := range []string{"one", "two", "three"} {
		if _, err := b.Reserve(ResourceRequest{ForemanID: "fm-a", Ticket: "bs-a", Task: task, Profile: "plan"}, 0); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := b.Status(0)
	if err != nil {
		t.Fatal(err)
	}
	first := initial.Leases[0].ID
	next := initial.Leases[1].ID
	if err := b.AdvanceReconciliation(first); err != nil {
		t.Fatal(err)
	}
	restarted := &ResourceBroker{Dir: b.Dir, Probe: b.Probe}
	for i := 0; i < 2; i++ {
		got, err := restarted.Status(0)
		if err != nil || got.Leases[0].ID != next {
			t.Fatalf("cursor did not advance: %+v %v", got, err)
		}
		if _, err := b.Release(first); err != nil {
			t.Fatal(err)
		}
	}
}
