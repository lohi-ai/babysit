package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/reallongnguyen/babysit/internal/identity"
)

// A malformed index.json is not an empty record. reconcileOne used to read it
// with the non-strict ReadDoc, see status "" → triage, and let applyStatus
// rewrite the file — destroying the corrupt bytes and the dashboard warning
// that reports them. The strict read must skip the ticket and leave the file
// byte-identical.
func TestReconcileSkipsMalformedIndex(t *testing.T) {
	proj := t.TempDir()
	th := filepath.Join(proj, "tickets", "bs-dead0001")
	if err := os.MkdirAll(th, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file that would advance a healthy ticket (triage → backlog), so the
	// only thing standing between the corrupt bytes and a rewrite is the read.
	if err := os.WriteFile(filepath.Join(th, "requirement.md"), []byte("req\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"id":"bs-dead0001","status":"triage",`)
	idx := filepath.Join(th, "index.json")
	if err := os.WriteFile(idx, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := reconcileOne(&out, identity.Env{ProjectHome: proj}, "bs-dead0001", false, false); err == nil {
		t.Fatal("reconcileOne on a malformed index must fail, not reconcile an empty record")
	}
	got, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("malformed index.json was rewritten:\n%s", got)
	}
}

// The stub case the strict read must not regress: a ticket dir with no
// index.json is skipped quietly and no file is created.
func TestReconcileSkipsMissingIndex(t *testing.T) {
	proj := t.TempDir()
	th := filepath.Join(proj, "tickets", "bs-dead0002")
	if err := os.MkdirAll(th, 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := reconcileOne(&out, identity.Env{ProjectHome: proj}, "bs-dead0002", false, false); err != nil {
		t.Fatalf("missing index.json is a skip, not an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(th, "index.json")); !os.IsNotExist(err) {
		t.Fatal("reconcile must not create index.json on a stub dir")
	}
}
