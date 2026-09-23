package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/orca"
)

const foremanResourceUsage = `Usage:
  bbs foreman resource status
  bbs foreman resource reserve <foreman-id> --ticket <ticket> --task <task> --profile <profile>
  bbs foreman resource release <lease-id>

Profiles: plan, standard, android-simulator, ios-simulator, local-ml
`

const defaultForemanMaxWorkers = 4

var newResourceBroker = foreman.DefaultResourceBroker

func foremanResource(args []string) error {
	if len(args) == 0 {
		fmt.Print(foremanResourceUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status":
		return foremanResourceStatus(rest)
	case "reserve":
		return foremanResourceReserve(rest)
	case "release":
		return foremanResourceRelease(rest)
	case "help", "--help", "-h":
		fmt.Print(foremanResourceUsage)
		return nil
	}
	return fmt.Errorf("foreman resource: unknown subcommand %q\n%s", sub, foremanResourceUsage)
}

func foremanResourceStatus(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("foreman resource status: unexpected arguments\n%s", foremanResourceUsage)
	}
	capUnits, err := configuredResourceCap()
	if err != nil {
		return err
	}
	broker := newResourceBroker()
	status, err := broker.Status(capUnits)
	if err != nil {
		return err
	}
	// status is the reconcile entry point every foreman wake runs, so the
	// cross-check lives here rather than in skill prose a caller can skip: a
	// lease whose Dispatch Orca proves terminal is released on the spot.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _ := orca.PreflightContext(ctx)
	released := reconcileResourceLeases(ctx, broker, status.Leases, c, time.Now())
	if len(released) > 0 {
		status, err = broker.Status(capUnits)
		if err != nil {
			return err
		}
	}
	printResourceStatus(status)
	for _, id := range released {
		fmt.Println("RELEASED_LEASE=" + id)
	}
	return nil
}

// Reconciliation is global and runs before admission, even if the original
// foreman vanished. No Orca I/O happens while the broker lock is held.
func reconcileResourceLeases(ctx context.Context, b *foreman.ResourceBroker, leases []foreman.ResourceLease, c *orca.Client, now time.Time) []string {
	if len(leases) == 0 {
		return nil
	}
	if c == nil || !c.Orchestration() {
		fmt.Fprintln(os.Stderr, "RESOURCE_RECOVERY=unavailable; existing worker reservations held")
		return nil
	}
	var released []string
	fleets := map[string]map[string]bool{}
	lastChecked := ""
	defer func() {
		if lastChecked != "" {
			if err := b.AdvanceReconciliation(lastChecked); err != nil {
				fmt.Fprintf(os.Stderr, "RESOURCE_RECOVERY_CHECKPOINT=%v\n", err)
			}
		}
	}()
	for _, lease := range leases {
		if ctx.Err() != nil {
			break
		}
		lastChecked = lease.ID
		d, err := c.DispatchStateFor(lease.Task)
		if err != nil {
			fmt.Fprintf(os.Stderr, "RESOURCE_HELD=%s REASON=%v\n", lease.ID, err)
			if errors.Is(err, context.DeadlineExceeded) {
				break
			}
			continue
		}
		// The terminal attempt observed before reserve belongs to the previous
		// generation, not the worker this reservation is about to launch.
		unstarted := d.ID == "" || d.ID == lease.PreviousDispatch
		reclaim := false
		if unstarted {
			lastReserved := lease.AcquiredAt
			if lease.RenewedAt != "" {
				lastReserved = lease.RenewedAt
			}
			acquired, err := time.Parse(time.RFC3339Nano, lastReserved)
			owner, ownerErr := foreman.Load(lease.ForemanID)
			heartbeat, heartbeatErr := time.Parse(time.RFC3339, owner.Heartbeat)
			stale := errors.Is(ownerErr, os.ErrNotExist) || (ownerErr == nil && (heartbeatErr != nil || now.Sub(heartbeat) >= foreman.StaleAfter))
			reclaim = err == nil && now.Sub(acquired) >= foreman.StaleAfter && stale
		} else if dispatchTerminal(d.Status) {
			reclaim = true
		} else if d.RunID != "" {
			exited, checked := fleets[d.RunID]
			if !checked {
				exited, err = c.ExitedWorkers(d.RunID)
				fleets[d.RunID] = exited
				if err != nil {
					fmt.Fprintf(os.Stderr, "RESOURCE_HELD=%s REASON=%v\n", lease.ID, err)
				}
			}
			if exited[d.ID] {
				// Stop targets an exact generation, never the Task's replacement.
				if err := c.StopExitedWorker(d.ID); err != nil {
					fmt.Fprintf(os.Stderr, "RESOURCE_HELD=%s REASON=%v\n", lease.ID, err)
					continue
				}
				settled, err := c.DispatchStateFor(lease.Task)
				reclaim = err == nil && settled.ID == d.ID && dispatchTerminal(settled.Status)
			} else {
				fmt.Fprintf(os.Stderr, "RESOURCE_HELD=%s DISPATCH=%s REASON=worker live or unverifiable\n", lease.ID, d.ID)
			}
		}
		if reclaim {
			if ok, err := b.ReleaseObserved(lease); err != nil {
				fmt.Fprintf(os.Stderr, "RESOURCE_HELD=%s REASON=%v\n", lease.ID, err)
			} else if ok {
				released = append(released, lease.ID)
			}
		}
	}
	return released
}

// dispatchTerminal mirrors the terminal half of Orca's dispatch_contexts
// status enum. 'pending', 'dispatched', and "" (no dispatch record) are all
// not terminal. A missing Dispatch is handled separately with launch grace
// and owner liveness evidence.
func dispatchTerminal(status string) bool {
	switch status {
	case "completed", "failed", "circuit_broken":
		return true
	}
	return false
}

func foremanResourceReserve(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("foreman resource reserve: needs a foreman id\n%s", foremanResourceUsage)
	}
	if _, err := foreman.Load(id); err != nil {
		return fmt.Errorf("foreman resource reserve: %w", err)
	}
	for _, key := range []string{"ticket", "task", "profile"} {
		if strings.TrimSpace(kv[key]) == "" {
			return fmt.Errorf("foreman resource reserve: --%s is required", key)
		}
	}
	capUnits, err := configuredResourceCap()
	if err != nil {
		return err
	}
	maxWorkers := defaultForemanMaxWorkers
	if value, ok := config.Get("parallel_max_workers"); ok && strings.TrimSpace(value) != "" {
		maxWorkers, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil || maxWorkers <= 0 {
			return fmt.Errorf("config parallel_max_workers needs a positive integer, got %q", value)
		}
	}
	broker := newResourceBroker()
	before, err := broker.Status(capUnits)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _ := orca.PreflightContext(ctx)
	previous := ""
	if c != nil && c.Orchestration() {
		d, err := c.DispatchStateFor(kv["task"])
		if err != nil {
			return fmt.Errorf("resource reserve: cannot inspect task: %w", err)
		}
		if dispatchTerminal(d.Status) {
			previous = d.ID
		}
	}
	released := reconcileResourceLeases(ctx, broker, before.Leases, c, time.Now())
	status, err := broker.Reserve(foreman.ResourceRequest{
		ForemanID:        id,
		Ticket:           kv["ticket"],
		Task:             kv["task"],
		Profile:          kv["profile"],
		MaxWorkers:       maxWorkers,
		PreviousDispatch: previous,
	}, capUnits)
	if err != nil {
		return err
	}
	printResourceStatus(status)
	for _, id := range released {
		fmt.Println("RELEASED_LEASE=" + id)
	}
	return nil
}

func foremanResourceRelease(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("foreman resource release: needs one lease id\n%s", foremanResourceUsage)
	}
	released, err := newResourceBroker().Release(args[0])
	if err != nil {
		return err
	}
	if released {
		fmt.Println("RELEASED=" + args[0])
	} else {
		fmt.Println("RELEASED=none")
	}
	return nil
}

func configuredResourceCap() (int, error) {
	value, ok := config.Get("parallel_global_units")
	value = strings.TrimSpace(value)
	if !ok || value == "" || value == "auto" {
		return 0, nil
	}
	units, err := strconv.Atoi(value)
	if err != nil || units <= 0 {
		return 0, fmt.Errorf("config parallel_global_units needs 'auto' or a positive integer, got %q", value)
	}
	return units, nil
}

func printResourceStatus(status foreman.ResourceStatus) {
	if status.Admission != "" {
		fmt.Println("ADMISSION=" + status.Admission)
	}
	if status.Reason != "" {
		fmt.Println("REASON=" + status.Reason)
	} else {
		fmt.Println("PRESSURE=ok")
	}
	if status.Lease != nil {
		fmt.Println("LEASE=" + status.Lease.ID)
		fmt.Println("PROFILE=" + status.Lease.Profile)
		fmt.Printf("UNITS=%d\n", status.Lease.Units)
	}
	fmt.Printf("GLOBAL_BUDGET=%d\n", status.Budget)
	fmt.Printf("GLOBAL_USED=%d\n", status.Used)
	fmt.Printf("HOST_CPUS=%d\n", status.Host.CPUs)
	fmt.Printf("HOST_TOTAL_MEMORY_BYTES=%d\n", status.Host.TotalMemoryBytes)
	if status.Host.MemoryKnown {
		fmt.Printf("HOST_AVAILABLE_MEMORY_BYTES=%d\n", status.Host.AvailableMemoryBytes)
	}
	if status.Host.LoadKnown {
		fmt.Printf("HOST_LOAD_1=%.2f\n", status.Host.Load1)
	}
	for _, lease := range status.Leases {
		fmt.Printf("ACTIVE_LEASE=%s FOREMAN=%s TICKET=%s PROFILE=%s UNITS=%d TASK=%s\n",
			lease.ID, lease.ForemanID, lease.Ticket, lease.Profile, lease.Units, lease.Task)
	}
}
