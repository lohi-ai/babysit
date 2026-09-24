package orca

import (
	"encoding/json"
	"errors"
)

// ExitedWorkers reads the fleet's agent verdict (not merely PTY liveness).
// Scope and pagination matter: an old foreman's workers may be on later pages.
func (c *Client) ExitedWorkers(run string) (map[string]bool, error) {
	if run == "" {
		return nil, errors.New("worker-list needs a run")
	}
	exited := map[string]bool{}
	cursor := ""
	seen := map[string]bool{}
	for {
		args := []string{"orchestration", "worker-list", "--run", run, "--include-remote"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		raw, err := c.run(args...)
		if err != nil {
			return nil, err
		}
		var page struct {
			Workers []struct {
				DispatchID     string `json:"dispatchId"`
				DispatchStatus string `json:"dispatchStatus"`
				TerminalState  string `json:"terminalState"`
				Projection     struct {
					Liveness struct {
						Verdict string `json:"verdict"`
					} `json:"liveness"`
				} `json:"projection"`
			} `json:"workers"`
			Page struct {
				HasMore    bool   `json:"hasMore"`
				NextCursor string `json:"nextCursor"`
			} `json:"page"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		for _, worker := range page.Workers {
			// A settled dispatch whose terminal was reused by a later dispatch
			// (retained) or already closed (released) cannot still be running:
			// its liveness row is gone with the terminal, which projects as
			// "unverifiable" even though the dispatch itself is completed.
			settled := worker.DispatchStatus == "completed" || worker.DispatchStatus == "failed"
			terminalGone := worker.TerminalState == "retained" || worker.TerminalState == "released"
			exited[worker.DispatchID] = worker.Projection.Liveness.Verdict == "exited" || (settled && terminalGone)
		}
		if !page.Page.HasMore {
			return exited, nil
		}
		cursor = page.Page.NextCursor
		if cursor == "" || seen[cursor] {
			return nil, errors.New("worker-list returned an invalid page cursor")
		}
		seen[cursor] = true
	}
}

// StopExitedWorker fences a proven exited attempt before capacity is reused.
// Callers must re-read Dispatch state; an acknowledged request alone is not
// proof that stop settled, especially on a remote host.
func (c *Client) StopExitedWorker(dispatch string) error {
	_, err := c.run("orchestration", "worker-stop", "--dispatch", dispatch)
	return err
}
