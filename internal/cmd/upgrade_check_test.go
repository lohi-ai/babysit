package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// `bbs upgrade check` is the version probe; a bare `bbs upgrade` pulls and
// relinks. They differ by one argv word, so a routing slip here is not a wrong
// answer but an unrequested upgrade. This pins the dispatch: with a stubbed
// remote and an isolated state dir, `upgrade check` must print the probe line
// and touch nothing else.
func TestUpgradeCheckRoutesToProbe(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("9.9.9\n"))
	}))
	defer remote.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BABYSIT_DIR", dir)
	t.Setenv("BABYSIT_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("BABYSIT_REMOTE_URL", remote.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"upgrade", "check"})

	// The probe writes to os.Stdout directly (byte-parity with the bash), so
	// capture the real fd rather than cobra's writer.
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	execErr := root.Execute()
	w.Close()
	os.Stdout = stdout

	var printed bytes.Buffer
	_, _ = printed.ReadFrom(r)

	if execErr != nil {
		t.Fatalf("upgrade check = %v, want nil", execErr)
	}
	if got := printed.String(); got != "UPGRADE_AVAILABLE 1.0.0 9.9.9\n" {
		t.Errorf("stdout = %q, want the probe line", got)
	}
}
