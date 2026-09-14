package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/reallongnguyen/babysit/internal/foreman"
)

const foremanResourceUsage = `Usage:
  bbs foreman resource status
  bbs foreman resource reserve <foreman-id> --ticket <ticket> --task <task> --profile <profile>
  bbs foreman resource release <lease-id>

Profiles: plan, standard, android-simulator, ios-simulator, local-ml
`

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
	released := reconcileResourceLeases(broker, status.Leases)
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

// reconcileResourceLeases releases each lease whose task's Dispatch Orca
// proves terminal. Orca unreachable, a probe error, a task with no dispatch
// record, and a live dispatch all leave the lease held: the failure mode is
// capacity loss, never a false release that could race two heavy workers onto
// one GPU/simulator stack.
func reconcileResourceLeases(b *foreman.ResourceBroker, leases []foreman.ResourceLease) []string {
	c := mailboxClient()
	if c == nil || len(leases) == 0 {
		return nil
	}
	var released []string
	for _, lease := range leases {
		status, err := c.DispatchStatusFor(lease.Task)
		if err != nil || !dispatchTerminal(status) {
			continue
		}
		if ok, err := b.Release(lease.ID); err == nil && ok {
			released = append(released, lease.ID)
		}
	}
	return released
}

// dispatchTerminal mirrors the terminal half of Orca's dispatch_contexts
// status enum. 'pending', 'dispatched', and "" (no dispatch record) are all
// unproven — a lease reserved before worker-start has no dispatch yet and is
// still owed to the retry.
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
	status, err := newResourceBroker().Reserve(foreman.ResourceRequest{
		ForemanID: id,
		Ticket:    kv["ticket"],
		Task:      kv["task"],
		Profile:   kv["profile"],
	}, capUnits)
	if err != nil {
		return err
	}
	printResourceStatus(status)
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
