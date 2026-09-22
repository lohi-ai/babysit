package orca

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExitedWorkersReadsEveryPageInExplicitRun(t *testing.T) {
	log := fakeOrca(t, `case "$1" in
 status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
 orchestration)
 case "$*" in
 *--cursor*) echo '{"ok":true,"result":{"workers":[{"dispatchId":"old","projection":{"liveness":{"verdict":"exited"}}}],"page":{"hasMore":false}}}' ;;
 *) echo '{"ok":true,"result":{"workers":[{"dispatchId":"live","projection":{"liveness":{"verdict":"live"}}},{"dispatchId":"unknown","projection":{"liveness":{"verdict":"unverifiable"}}}],"page":{"hasMore":true,"nextCursor":"page2"}}}' ;;
 esac ;;
 esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	exited, err := c.ExitedWorkers("run-other-foreman")
	if err != nil {
		t.Fatal(err)
	}
	if !exited["old"] || exited["live"] || exited["unknown"] {
		t.Fatalf("wrong evidence: %+v", exited)
	}
	if strings.Count(calls(t, log), "--run run-other-foreman --include-remote") != 2 {
		t.Fatalf("lost scope: %s", calls(t, log))
	}
}

func TestResourcePreflightDeadlineBoundsHungCLI(t *testing.T) {
	fakeOrca(t, `exec sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := PreflightContext(ctx); err == nil {
		t.Fatal("hung CLI succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("preflight exceeded its deadline")
	}
}

func TestDispatchStateRejectsMissingEvidence(t *testing.T) {
	fakeOrca(t, `case "$1" in
 status) echo '{"ok":true,"result":{"runtime":{"reachable":true,"capabilities":["orchestration.contract.v1"]}}}' ;;
 *) echo '{"ok":true,"result":{}}' ;;
 esac`)
	c, err := Preflight()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DispatchStateFor("task"); err == nil {
		t.Fatal("missing field mistaken for no dispatch")
	}
}
