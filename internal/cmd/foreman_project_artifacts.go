package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reallongnguyen/babysit/internal/ticket"
)

func projectArtifactInputs(st *ticket.Store, artifacts map[string]string) (string, error) {
	doc, err := ticket.ReadDocStrict(st.IndexPath())
	if err != nil {
		return "", err
	}
	parentID := doc.Get("parent")
	if !idRe.MatchString(parentID) {
		return "", fmt.Errorf("artifact child needs a parent")
	}
	parent := storeForTicket(st.Env, parentID)
	var c projectContract
	if err := readProjectJSON(projectContractPath(parent), &c); err != nil || !c.ArtifactsOnly {
		return "", fmt.Errorf("artifact seal requires an approved artifacts_only project")
	}
	if state, _, _ := approvalRead(parent); state != "approved" {
		return "", fmt.Errorf("artifact project approval is not current")
	}
	workflow := doc.Get("pointers.workflow")
	if !idRe.MatchString(workflow) {
		return "", fmt.Errorf("artifact child needs pointers.workflow")
	}
	status := verdictStatus(st, workflow)
	if status != "DONE" && status != "DONE_WITH_CONCERNS" {
		return "", fmt.Errorf("artifact workflow verdict is not complete")
	}
	// Evidence-only delivery must never conceal production commits.
	if m, err := ticket.ReadManifest(st.ManifestPath()); err == nil {
		for _, repo := range m.Repos {
			p, err := resolveGitFlow(repo.Canonical)
			if err != nil {
				return "", err
			}
			if gitOutIn(repo.Canonical, "rev-list", "--count", p.BaseBranch+".."+repo.Branch) != "0" {
				return "", fmt.Errorf("artifact project contains code commits")
			}
			if repo.Worktree != "" && gitOutIn(repo.Worktree, "status", "--porcelain", "--untracked-files=all") != "" {
				return "", fmt.Errorf("artifact worktree contains uncommitted changes")
			}
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	rows := map[string]string{}
	for _, name := range []string{"requirement.md", "plan.md"} {
		if d, ok := digestFile(filepath.Join(st.Home(), name)); ok {
			rows[name] = d
		}
	}
	rows["verdict"], _ = digestFile(st.VerdictPath(workflow))
	for path, expected := range artifacts {
		resolved, err := resolveEvidencePath(path, st.Home())
		if err != nil {
			return "", err
		}
		digest, ok := digestFile(resolved)
		if !ok || digest != expected {
			return "", fmt.Errorf("artifact changed or unavailable: %s", path)
		}
		rows[path] = digest
	}
	return digestJSON(rows), nil
}

func validateArtifactSeal(st *ticket.Store, seal projectSeal) []string {
	inputs, err := projectArtifactInputs(st, seal.Artifacts)
	if err != nil {
		return []string{err.Error()}
	}
	if seal.Version != 1 || seal.Ticket != st.Env.Ticket || inputs != seal.Inputs {
		return []string{"artifact seal no longer matches inputs"}
	}
	return nil
}

func projectSealArtifacts(st *ticket.Store, file string) error {
	var input struct {
		Paths []string `json:"paths"`
	}
	if err := readProjectJSON(file, &input); err != nil {
		return err
	}
	if len(input.Paths) == 0 {
		return fmt.Errorf("artifact seal needs paths inside the child ticket")
	}
	return withLock(st, func() error {
		seal := projectSeal{Version: 1, Ticket: st.Env.Ticket, At: isoNow(), Artifacts: map[string]string{}, Owners: []string{}}
		for _, path := range input.Paths {
			resolved, err := resolveEvidencePath(path, st.Home())
			if err != nil {
				return err
			}
			digest, ok := digestFile(resolved)
			if !ok {
				return fmt.Errorf("artifact unavailable: %s", path)
			}
			seal.Artifacts[resolved] = digest
		}
		inputs, err := projectArtifactInputs(st, seal.Artifacts)
		if err != nil {
			return err
		}
		seal.Inputs = inputs
		return writeJSONAtomic(filepath.Join(st.Home(), "project-seal.json"), seal)
	})
}
