package foreman

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/identity"
)

const (
	resourceStateVersion = 1
	resourceLockRetries  = 50
	resourceLockDelay    = 100 * time.Millisecond
	gib                  = uint64(1024 * 1024 * 1024)
)

// ResourceProfile is the host load a worker reserves before it starts. Units
// account for CPU and memory together; exclusive resources prevent the laptop's
// shared GPU and simulator stack from being multiplied by independent foremen.
type ResourceProfile struct {
	Name      string
	Units     int
	Exclusive []string
}

var resourceProfiles = map[string]ResourceProfile{
	"plan":              {Name: "plan", Units: 1},
	"standard":          {Name: "standard", Units: 2},
	"android-simulator": {Name: "android-simulator", Units: 4, Exclusive: []string{"mobile-simulator", "gpu-heavy"}},
	"ios-simulator":     {Name: "ios-simulator", Units: 4, Exclusive: []string{"mobile-simulator", "gpu-heavy"}},
	"local-ml":          {Name: "local-ml", Units: 4, Exclusive: []string{"gpu-heavy"}},
}

// HostResources is one admission-time sample. Known flags distinguish a real
// zero from a platform where that pressure signal is unavailable.
type HostResources struct {
	CPUs                 int
	TotalMemoryBytes     uint64
	AvailableMemoryBytes uint64
	Load1                float64
	MemoryKnown          bool
	LoadKnown            bool
}

// ResourceRequest identifies one pending Orca Task. ForemanID + Task is the
// idempotency key, so a crash after reservation can safely retry the command.
type ResourceRequest struct {
	ForemanID        string
	Ticket           string
	Task             string
	Profile          string
	MaxWorkers       int
	PreviousDispatch string
}

// ResourceLease is durable machine-global capacity held by a worker.
type ResourceLease struct {
	ID               string   `json:"id"`
	ForemanID        string   `json:"foreman_id"`
	Ticket           string   `json:"ticket"`
	Task             string   `json:"task"`
	Profile          string   `json:"profile"`
	Units            int      `json:"units"`
	Exclusive        []string `json:"exclusive,omitempty"`
	AcquiredAt       string   `json:"acquired_at"`
	PreviousDispatch string   `json:"previous_dispatch,omitempty"`
	RenewedAt        string   `json:"renewed_at,omitempty"`
}

// ResourceStatus is the complete admission snapshot returned by reserve and
// status. A queued reservation never mutates Leases.
type ResourceStatus struct {
	Admission string
	Reason    string
	Budget    int
	Used      int
	Host      HostResources
	Lease     *ResourceLease
	Leases    []ResourceLease
}

type resourceState struct {
	Version        int             `json:"version"`
	Leases         []ResourceLease `json:"leases"`
	ReconcileAfter string          `json:"reconcile_after,omitempty"`
}

// ResourceBroker serializes reservations from every foreman through one state
// file under BABYSIT_HOME. Probe is injectable so admission behavior is
// deterministic in tests.
type ResourceBroker struct {
	Dir   string
	Probe func() HostResources
	Now   func() time.Time
}

func DefaultResourceBroker() *ResourceBroker {
	return &ResourceBroker{
		Dir:   filepath.Join(identity.BabysitHome(), "resources"),
		Probe: ProbeHostResources,
		Now:   time.Now,
	}
}

func ResourceProfileNames() []string {
	names := make([]string, 0, len(resourceProfiles))
	for name := range resourceProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Reserve atomically admits a task or reports why it remains queued. capUnits
// is an optional operator ceiling; zero uses the host-derived budget.
func (b *ResourceBroker) Reserve(req ResourceRequest, capUnits int) (ResourceStatus, error) {
	profile, err := validateResourceRequest(req)
	if err != nil {
		return ResourceStatus{}, err
	}
	if capUnits < 0 {
		return ResourceStatus{}, errors.New("resource budget must be positive or zero for auto")
	}
	host := b.probe()
	budget := resourceBudget(host, capUnits)

	var result ResourceStatus
	err = b.withLock(func() error {
		state, err := b.load()
		if err != nil {
			return err
		}
		used := resourceUnits(state.Leases)
		result = ResourceStatus{Budget: budget, Used: used, Host: host, Leases: append([]ResourceLease(nil), state.Leases...)}

		for i := range state.Leases {
			lease := &state.Leases[i]
			if lease.ForemanID == req.ForemanID && lease.Task == req.Task {
				if lease.Ticket != req.Ticket || lease.Profile != req.Profile {
					return fmt.Errorf("resource task %s/%s already reserved for ticket %s with profile %s", req.ForemanID, req.Task, lease.Ticket, lease.Profile)
				}
				lease.RenewedAt = b.now().UTC().Format(time.RFC3339Nano)
				if err := b.save(state); err != nil {
					return err
				}
				result.Leases = append([]ResourceLease(nil), state.Leases...)
				copy := *lease
				result.Admission = "reserved"
				result.Lease = &copy
				return nil
			}
		}
		if req.MaxWorkers > 0 {
			count := 0
			for _, lease := range state.Leases {
				if lease.ForemanID == req.ForemanID {
					count++
				}
			}
			if count >= req.MaxWorkers {
				result.Admission = "queued"
				result.Reason = fmt.Sprintf("foreman worker limit reached: %d of %d", count, req.MaxWorkers)
				return nil
			}
		}

		if reason := resourcePressureReason(host); reason != "" {
			result.Admission = "queued"
			result.Reason = reason
			return nil
		}
		if conflict := resourceExclusiveConflict(state.Leases, profile.Exclusive); conflict != "" {
			result.Admission = "queued"
			result.Reason = "exclusive resource busy: " + conflict
			return nil
		}
		if used+profile.Units > budget {
			result.Admission = "queued"
			result.Reason = fmt.Sprintf("global capacity unavailable: need %d units, %d of %d used", profile.Units, used, budget)
			return nil
		}

		id, err := resourceLeaseID()
		if err != nil {
			return err
		}
		lease := ResourceLease{
			ID:               id,
			ForemanID:        req.ForemanID,
			Ticket:           req.Ticket,
			Task:             req.Task,
			Profile:          profile.Name,
			Units:            profile.Units,
			Exclusive:        append([]string(nil), profile.Exclusive...),
			AcquiredAt:       b.now().UTC().Format(time.RFC3339),
			PreviousDispatch: req.PreviousDispatch,
		}
		state.Leases = append(state.Leases, lease)
		sort.Slice(state.Leases, func(i, j int) bool { return state.Leases[i].ID < state.Leases[j].ID })
		if err := b.save(state); err != nil {
			return err
		}
		result.Admission = "reserved"
		result.Used = used + profile.Units
		result.Lease = &lease
		result.Leases = append([]ResourceLease(nil), state.Leases...)
		return nil
	})
	return result, err
}

// Release is idempotent. Each reservation generation has a unique ID, so a
// delayed release cannot remove a replacement reservation for the same Task.
func (b *ResourceBroker) Release(id string) (bool, error) {
	return b.release(id, nil)
}

// ReleaseObserved rejects a stale reconciliation snapshot if the owner renewed
// its launch reservation while Orca was being queried outside the lock.
func (b *ResourceBroker) ReleaseObserved(observed ResourceLease) (bool, error) {
	return b.release(observed.ID, &observed)
}

func (b *ResourceBroker) release(id string, observed *ResourceLease) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, errors.New("resource release needs a lease id")
	}
	released := false
	err := b.withLock(func() error {
		state, err := b.load()
		if err != nil {
			return err
		}
		kept := state.Leases[:0]
		for _, lease := range state.Leases {
			if lease.ID == id && (observed == nil || lease.RenewedAt == observed.RenewedAt) {
				released = true
				continue
			}
			kept = append(kept, lease)
		}
		if !released {
			return nil
		}
		state.Leases = kept
		return b.save(state)
	})
	return released, err
}

func (b *ResourceBroker) Status(capUnits int) (ResourceStatus, error) {
	if capUnits < 0 {
		return ResourceStatus{}, errors.New("resource budget must be positive or zero for auto")
	}
	host := b.probe()
	state, err := b.load()
	if err != nil {
		return ResourceStatus{}, err
	}
	// Resume after the last attempted lease, so a slow/unreachable worker
	// cannot exhaust every tick's probe budget before peers get inspected.
	leases := append([]ResourceLease(nil), state.Leases...)
	for i, lease := range leases {
		if lease.ID > state.ReconcileAfter {
			leases = append(append([]ResourceLease(nil), leases[i:]...), leases[:i]...)
			break
		}
	}
	return ResourceStatus{
		Budget: resourceBudget(host, capUnits),
		Used:   resourceUnits(state.Leases),
		Host:   host,
		Reason: resourcePressureReason(host),
		Leases: leases,
	}, nil
}

// AdvanceReconciliation checkpoints probe progress independently of the owner.
func (b *ResourceBroker) AdvanceReconciliation(id string) error {
	return b.withLock(func() error {
		state, err := b.load()
		if err != nil {
			return err
		}
		state.ReconcileAfter = id
		return b.save(state)
	})
}

func validateResourceRequest(req ResourceRequest) (ResourceProfile, error) {
	if err := ValidID(req.ForemanID); err != nil {
		return ResourceProfile{}, err
	}
	if strings.TrimSpace(req.Ticket) == "" {
		return ResourceProfile{}, errors.New("resource reserve needs a ticket")
	}
	if strings.TrimSpace(req.Task) == "" {
		return ResourceProfile{}, errors.New("resource reserve needs a task")
	}
	profile, ok := resourceProfiles[req.Profile]
	if !ok {
		return ResourceProfile{}, fmt.Errorf("unknown resource profile %q (want %s)", req.Profile, strings.Join(ResourceProfileNames(), ", "))
	}
	return profile, nil
}

func resourceBudget(host HostResources, capUnits int) int {
	cpus := host.CPUs
	if cpus < 1 {
		cpus = 1
	}
	budget := cpus
	if host.TotalMemoryBytes > 0 {
		totalGiB := int(host.TotalMemoryBytes / gib)
		ramBudget := totalGiB - 6
		if ramBudget < 1 {
			ramBudget = 1
		}
		if ramBudget < budget {
			budget = ramBudget
		}
	}
	if capUnits > 0 && capUnits < budget {
		budget = capUnits
	}
	return budget
}

func resourcePressureReason(host HostResources) string {
	if host.MemoryKnown && host.TotalMemoryBytes > 0 && host.AvailableMemoryBytes*100 < host.TotalMemoryBytes*20 {
		return "memory pressure is high (less than 20% available)"
	}
	if host.LoadKnown && host.CPUs > 0 && host.Load1 >= float64(host.CPUs)*0.8 {
		return fmt.Sprintf("CPU pressure is high (load %.2f across %d CPUs)", host.Load1, host.CPUs)
	}
	return ""
}

func resourceUnits(leases []ResourceLease) int {
	used := 0
	for _, lease := range leases {
		used += lease.Units
	}
	return used
}

func resourceExclusiveConflict(leases []ResourceLease, wanted []string) string {
	for _, lease := range leases {
		for _, held := range lease.Exclusive {
			for _, resource := range wanted {
				if held == resource {
					return resource
				}
			}
		}
	}
	return ""
}

func resourceLeaseID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return "rsc-" + hex.EncodeToString(id[:]), nil
}

func (b *ResourceBroker) statePath() string { return filepath.Join(b.Dir, "foreman-leases.json") }
func (b *ResourceBroker) lockPath() string  { return filepath.Join(b.Dir, ".foreman-leases.flock") }

func (b *ResourceBroker) probe() HostResources {
	if b.Probe == nil {
		return ProbeHostResources()
	}
	return b.Probe()
}

func (b *ResourceBroker) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

func (b *ResourceBroker) withLock(fn func() error) error {
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return err
	}
	release, err := lockResources(b.lockPath())
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func (b *ResourceBroker) load() (resourceState, error) {
	state := resourceState{Version: resourceStateVersion}
	body, err := os.ReadFile(b.statePath())
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return state, fmt.Errorf("resource state: %w", err)
	}
	if state.Version != resourceStateVersion {
		return state, fmt.Errorf("resource state version %d is unsupported", state.Version)
	}
	return state, nil
}

func (b *ResourceBroker) save(state resourceState) error {
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(b.Dir, ".foreman-leases.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), b.statePath())
}

// ProbeHostResources samples the host without retaining a daemon. Unsupported
// signals remain unknown; the static CPU/RAM budget still bounds admission.
func ProbeHostResources() HostResources {
	host := HostResources{CPUs: runtime.NumCPU()}
	switch runtime.GOOS {
	case "darwin":
		probeDarwinResources(&host)
	case "linux":
		probeLinuxResources(&host)
	}
	return host
}

func probeDarwinResources(host *HostResources) {
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
			host.TotalMemoryBytes = n
		}
	}
	if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		fields := strings.Fields(strings.Trim(string(out), "{} \n\t"))
		if len(fields) > 0 {
			if load, err := strconv.ParseFloat(fields[0], 64); err == nil {
				host.Load1 = load
				host.LoadKnown = true
			}
		}
	}
	if out, err := exec.Command("memory_pressure", "-Q").Output(); err == nil && host.TotalMemoryBytes > 0 {
		const marker = "System-wide memory free percentage:"
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, marker) {
				continue
			}
			value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, marker), "%"))
			if pct, err := strconv.ParseUint(value, 10, 64); err == nil && pct <= 100 {
				host.AvailableMemoryBytes = host.TotalMemoryBytes * pct / 100
				host.MemoryKnown = true
			}
		}
	}
}

func probeLinuxResources(host *HostResources) {
	if body, err := os.ReadFile("/proc/loadavg"); err == nil {
		if fields := strings.Fields(string(body)); len(fields) > 0 {
			if load, err := strconv.ParseFloat(fields[0], 64); err == nil {
				host.Load1 = load
				host.LoadKnown = true
			}
		}
	}
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer file.Close()
	var totalKB, availableKB uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB = value
		case "MemAvailable":
			availableKB = value
		}
	}
	if totalKB > 0 {
		host.TotalMemoryBytes = totalKB * 1024
	}
	if totalKB > 0 && availableKB > 0 {
		host.AvailableMemoryBytes = availableKB * 1024
		host.MemoryKnown = true
	}
}
