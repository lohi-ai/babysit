package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestAutopilotV2SnapshotIsReadOnlyAndTracksWorkingTree(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	t.Setenv("BBS_TICKET", "ap-02")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "ap-02")
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "index.json"), `{"id":"ap-02","origin":{"type":"standalone"},"control":null}`)
	mustWrite(t, filepath.Join(home, "checkpoint.json"), `{"ticket":"ap-02","workflow":"builder","revision":7}`)
	mustWrite(t, filepath.Join(home, "requirement.md"), "requirement\n")
	mustWrite(t, filepath.Join(home, "plan.md"), "plan\n")

	a := &apState{slug: "project", branch: "feat/ap-02_snapshot", ticket: "ap-02", stateRoot: project}
	before := directoryFingerprint(t, project)
	first, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	after := directoryFingerprint(t, project)
	if before != after {
		t.Fatalf("snapshot mutated ticket state: before=%s after=%s", before, after)
	}
	if first.StateRevision != 7 || first.Run == nil || first.Run.Mode != "implement" {
		t.Fatalf("unexpected route facts: revision=%d run=%+v", first.StateRevision, first.Run)
	}
	if first.Git.Dirty {
		t.Fatalf("clean repo reported dirty: %+v", first.Git)
	}

	mustWrite(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	mustWrite(t, filepath.Join(repo, "untracked.txt"), "new\n")
	second, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Git.Dirty {
		t.Fatal("tracked/untracked changes did not mark snapshot dirty")
	}
	if second.Git.TreeDigest == first.Git.TreeDigest || second.SnapshotID == first.SnapshotID {
		t.Fatal("working-tree changes did not invalidate snapshot identity")
	}
}

func TestAutopilotV2SnapshotNoTicketDoesNotInitializeState(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	t.Setenv("BBS_TICKET", "")
	t.Setenv("BABYSIT_TICKET", "")
	project := filepath.Join(t.TempDir(), "missing-project")
	a := &apState{slug: "project", branch: "main", stateRoot: project}

	s, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Ticket != nil || s.Run != nil {
		t.Fatalf("no-ticket snapshot invented durable state: ticket=%+v run=%+v", s.Ticket, s.Run)
	}
	if _, err := os.Stat(project); !os.IsNotExist(err) {
		t.Fatalf("snapshot initialized project state: %v", err)
	}
}

func TestAutopilotV2SnapshotDistinguishesRemoteExistsFromExactPush(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", remote)
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-u", "origin", "main")

	a := &apState{slug: "project", branch: "main", stateRoot: filepath.Join(t.TempDir(), "state")}
	pushed, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if !pushed.Git.PushedExactly || pushed.Git.RemoteHead == "" {
		t.Fatalf("exact push not observed: %+v", pushed.Git)
	}

	mustWrite(t, filepath.Join(repo, "tracked.txt"), "second\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "second")
	local, err := collectAutopilotSnapshot(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if local.Git.RemoteHead == "" || local.Git.PushedExactly {
		t.Fatalf("remote existence was confused with exact push: %+v", local.Git)
	}
}

func TestAutopilotV2SnapshotRejectsMalformedRequiredState(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Chdir(repo)
	t.Setenv("BBS_BASE_BRANCH", "main")
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(project, "tickets", "broken")
	mustMkdirAll(t, home)
	mustWrite(t, filepath.Join(home, "index.json"), "{")
	a := &apState{slug: "project", branch: "feat/broken_x", ticket: "broken", stateRoot: project}
	_, err := collectAutopilotSnapshot(a, "")
	var got string
	if se, ok := err.(*snapshotError); ok {
		got = se.Code
	}
	if got != "STATE_MALFORMED" {
		t.Fatalf("malformed state error=%v, want STATE_MALFORMED", err)
	}
}

func TestGitFlowV2ProvenanceUsesAuthoritativeResolver(t *testing.T) {
	repo := initSnapshotRepo(t)
	t.Setenv("BBS_BASE_BRANCH", "")
	mustMkdirAll(t, filepath.Join(repo, ".babysit"))
	mustWrite(t, filepath.Join(repo, ".babysit", "git-flow.yaml"), "profile: startup\nfinish: review\npush: false\n")
	p, err := snapshotGitFlow(repo)
	if err != nil {
		t.Fatal(err)
	}
	if p.Effective["profile"] != "startup" || p.Effective["push"] != "false" {
		t.Fatalf("wrong effective policy: %+v", p.Effective)
	}
	if p.Provenance["profile"] != "git-flow.yaml:profile" || p.Provenance["push"] != "git-flow.yaml:push" {
		t.Fatalf("wrong provenance: %+v", p.Provenance)
	}
	if !strings.HasPrefix(p.Digest, "sha256:") {
		t.Fatalf("missing policy digest: %q", p.Digest)
	}
}

func initSnapshotRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, "", "init", "-b", "main", repo)
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "first\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "first")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	if dir != "" {
		c.Dir = dir
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func directoryFingerprint(t *testing.T, root string) string {
	t.Helper()
	var rows []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		row := rel + "|" + info.Mode().String()
		if info.Mode().IsRegular() {
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sum := sha256.Sum256(b)
			row += "|" + hex.EncodeToString(sum[:])
		}
		rows = append(rows, row)
		return nil
	})
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:])
}
