package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/reallongnguyen/babysit/internal/config"
	"github.com/reallongnguyen/babysit/internal/foreman"
)

func resourceCLIFixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BABYSIT_HOME", home)
	t.Setenv("BABYSIT_STATE_DIR", home)
	if err := foreman.Save(foreman.Record{ID: "fm-a", Heartbeat: foreman.Now()}); err != nil {
		t.Fatal(err)
	}
	old := newResourceBroker
	newResourceBroker = func() *foreman.ResourceBroker {
		total := uint64(16 * 1024 * 1024 * 1024)
		return &foreman.ResourceBroker{
			Dir: filepath.Join(home, "resources"),
			Probe: func() foreman.HostResources {
				return foreman.HostResources{
					CPUs: 8, TotalMemoryBytes: total, AvailableMemoryBytes: total / 2,
					Load1: 1, MemoryKnown: true, LoadKnown: true,
				}
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
	if !strings.Contains(out, "ADMISSION=reserved\n") || !strings.Contains(out, "GLOBAL_BUDGET=4\n") {
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
