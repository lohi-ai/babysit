package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reallongnguyen/babysit/internal/agent"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/orca"
)

// Watchdog for a foreman that stopped moving — and a metronome for one that
// hasn't.
//
// A foreman drives its batch from its own terminal, so the failure mode nobody
// sees is the quiet one: the session finishes a thought, prints nothing more,
// and sits at an idle prompt while its workers wait for a design gate. No
// component notices, because nothing failed — the record's heartbeat is written
// by the foreman itself, so a foreman that stopped working also stopped
// reporting that it stopped.
//
// The only signal that survives that is the one from outside: the terminal. If
// the last N lines of the pane are byte-identical for long enough, the session
// is not working, whatever it last said about itself. The response is the
// cheapest thing that could restart it — type "check status" into the pane, the
// same nudge a human would give — and the escalation is to stop nudging and say
// so, because a watchdog that pokes forever is indistinguishable from one that
// is broken.
//
// Beside that stall clock runs a second, independent one: every
// --status-interval the foreman gets the same skill prompt even while its pane
// is moving, because a busy pane is not a status report. The two clocks never
// share state — a status prompt spends no nudge budget and buys no idle time,
// so it cannot let a dead terminal evade the bound. A foreman that reported
// itself done leaves the loop entirely.
//
// Deliberately not a daemon: it is a foreground loop (or a single --once pass
// for cron), holds no lock, and writes only its own state file. Nothing else in
// babysit depends on it running.

// watchState is the per-foreman clock, on disk so `--once` from cron measures
// the same idle window a long-running loop does. Losing it costs one idle
// period, so it is written best-effort and never fatal.
type watchState struct {
	Fingerprint string `json:"fingerprint"`
	// Since is when the pane last changed — the start of the current idle window.
	Since string `json:"since"`
	// Nudges counts consecutive nudges that have not produced independent
	// progress; it is the budget --max-nudges spends.
	Nudges int `json:"nudges"`
	// Pending marks a nudge whose own echo has not been seen yet. Without it the
	// watchdog nudges forever: the nudge itself changes the pane, that change
	// reads as progress, the counter resets, and --max-nudges never binds.
	Pending bool `json:"pending_nudge,omitempty"`
	// PendingStatus is Pending's twin for the periodic status check: the
	// prompt's own echo must not read as independent progress either.
	PendingStatus bool `json:"pending_status,omitempty"`
	// StatusCheck is when the last status prompt was sent — the start of the
	// current --status-interval window. It is a second clock beside Since:
	// the idle window measures the pane, this one measures the calendar.
	StatusCheck string `json:"status_check,omitempty"`
	// Stalled records that the budget ran out and was reported, so the loop says
	// it once rather than every interval.
	Stalled bool `json:"stalled,omitempty"`
}

type watchOpts struct {
	interval       time.Duration
	idle           time.Duration
	statusInterval time.Duration
	lines          int
	nudge          string
	maxNudges      int
	once           bool
}

func watchDir() string { return filepath.Join(identity.BabysitHome(), "watch") }

func watchLoad(id string) watchState {
	var s watchState
	b, err := os.ReadFile(filepath.Join(watchDir(), id+".json"))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func watchSave(id string, s watchState) {
	if err := os.MkdirAll(watchDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(watchDir(), id+".json"), b, 0o644)
}

// paneFingerprint hashes a captured pane. Only trailing whitespace is stripped,
// per line: terminals pad short lines to the window width, so the same content
// hashes differently after a resize. Nothing else is normalized — a spinner
// that keeps ticking is a session that is working, and filtering it out is how
// a watchdog starts calling live work stalled.
func paneFingerprint(pane string) string {
	lines := strings.Split(pane, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t\r")
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

func watchOptsFrom(kv map[string]string) (watchOpts, error) {
	o := watchOpts{
		interval:       60 * time.Second,
		idle:           10 * time.Minute,
		statusInterval: 15 * time.Minute,
		lines:          40,
		nudge:          "check status",
		maxNudges:      3,
	}
	secs := func(key string, dst *time.Duration) error {
		v, ok := kv[key]
		if !ok {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("foreman watch: --%s needs a positive number of seconds, got '%s'", key, v)
		}
		*dst = time.Duration(n) * time.Second
		return nil
	}
	if err := secs("interval", &o.interval); err != nil {
		return o, err
	}
	if err := secs("idle", &o.idle); err != nil {
		return o, err
	}
	if err := secs("status-interval", &o.statusInterval); err != nil {
		return o, err
	}
	if v, ok := kv["lines"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return o, fmt.Errorf("foreman watch: --lines needs a positive number, got '%s'", v)
		}
		o.lines = n
	}
	if v, ok := kv["max-nudges"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return o, fmt.Errorf("foreman watch: --max-nudges needs a non-negative number, got '%s'", v)
		}
		o.maxNudges = n
	}
	if v, ok := kv["nudge"]; ok {
		if strings.TrimSpace(v) == "" {
			return o, errors.New("foreman watch: --nudge needs text to send")
		}
		o.nudge = v
	}
	o.once = kv["once"] == "1"
	return o, nil
}

func foremanWatch(args []string) error {
	id, kv, err := foremanFlags(args)
	if err != nil {
		return err
	}
	o, err := watchOptsFrom(kv)
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := foreman.Load(id); err != nil {
			return err
		}
	}
	client, err := orca.Preflight()
	if err != nil {
		return err
	}
	for {
		targets, err := watchTargets(client, id)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			// Nothing to watch is a terminal condition, not an error: the
			// foreman finished and its terminal closed, which is the outcome
			// the batch was aiming for.
			fmt.Println("watch: no foreman with an open Orca terminal — nothing to watch")
			return nil
		}
		for _, r := range targets {
			if line := watchTick(client, r, o, time.Now()); line != "" {
				fmt.Println(line)
			}
		}
		if o.once {
			return nil
		}
		time.Sleep(o.interval)
	}
}

// watchTargets is the set to poll: one named foreman, or every registered one
// whose Orca terminal is still open.
//
// "Open terminal", not Live() — a foreman's heartbeat is written by the
// foreman, so a session wedged long enough to need a nudge is exactly the one
// that has gone stale. Selecting on liveness would drop every foreman this
// command exists to catch.
func watchTargets(client *orca.Client, id string) ([]foreman.Record, error) {
	var records []foreman.Record
	if id != "" {
		r, err := foreman.Load(id)
		if err != nil {
			return nil, err
		}
		records = []foreman.Record{r}
	} else {
		records = foreman.List()
	}
	var open []foreman.Record
	for _, r := range records {
		// A foreman that reported itself done leaves the loop even while its
		// terminal stays open — the batch closed, the pane is just a leftover.
		if r.Status == "done" {
			continue
		}
		if r.WorkspaceTitle == "" {
			continue
		}
		if _, err := client.Ref(r.WorkspaceTitle); err == nil {
			open = append(open, r)
		}
	}
	return open, nil
}

// watchTick is one poll of one foreman. It returns the line to print, or "" for
// "nothing a human needs to know" — a moving foreman is the normal case and
// must not produce output every interval, or the signal drowns.
func watchTick(client *orca.Client, r foreman.Record, o watchOpts, now time.Time) string {
	pane, err := client.CapturePane(r.WorkspaceTitle, o.lines)
	if err != nil {
		if errors.Is(err, orca.ErrNoTerminal) {
			return fmt.Sprintf("GONE %s — terminal %q is closed", r.ID, r.WorkspaceTitle)
		}
		foreman.MarkUnreachable(r.ID)
		return fmt.Sprintf("UNREACHABLE %s — %s", r.ID, err)
	}
	foreman.ClearUnreachable(r.ID)

	fp := paneFingerprint(pane)
	s := watchLoad(r.ID)
	moved := ""
	if s.Fingerprint != fp {
		moved = "MOVING"
		if s.Pending {
			// The pane changed for the first time since we nudged, so this is
			// most likely our own text echoing. Restart the idle clock but keep
			// the nudge budget spent: real work changes the pane on more than
			// one tick, and the tick after this one is what clears the counter.
			s.Pending = false
			moved = "NUDGE-ECHO"
		} else if s.PendingStatus {
			// Same attribution for the status prompt's own echo.
			s.PendingStatus = false
			moved = "STATUS-ECHO"
		} else {
			s.Nudges, s.Stalled = 0, false
		}
		s.Fingerprint = fp
		// The status echo is the one pane change that must not restart the
		// idle clock: the prompt is a check, not proof of life, and resetting
		// Since here would let a dead-but-echoing terminal slip the stall
		// bound every --status-interval.
		if moved != "STATUS-ECHO" {
			s.Since = now.UTC().Format(time.RFC3339)
		}
		watchSave(r.ID, s)
	}

	// The status clock is a second, independent timer: it fires on the
	// calendar even while the pane keeps moving, so a busy foreman still gets
	// asked for status. It never touches Since — a status prompt is a check,
	// not the stall response, and must not defer the nudge a dead pane owes.
	// A stalled foreman is already reported; poking it further is the
	// watchdog that never stops.
	statusDue := false
	if !s.Stalled {
		last, err := time.Parse(time.RFC3339, s.StatusCheck)
		if err != nil {
			// First sighting (or an unreadable stamp): start the clock now
			// rather than prompting on the very first tick.
			s.StatusCheck = now.UTC().Format(time.RFC3339)
			watchSave(r.ID, s)
		} else {
			statusDue = now.Sub(last) >= o.statusInterval
		}
	}

	since, err := time.Parse(time.RFC3339, s.Since)
	if err != nil {
		// First sighting of this pane (or an unreadable stamp): start the clock
		// now rather than treating an unknown age as infinite and nudging on the
		// very first tick.
		s.Since = now.UTC().Format(time.RFC3339)
		watchSave(r.ID, s)
		if o.once {
			return fmt.Sprintf("MOVING %s", r.ID)
		}
		return ""
	}
	idleFor := now.Sub(since)

	// The stall verdict outranks the status clock: a pane that spent its
	// nudge budget is reported dead, not kept on the prompting metronome.
	if moved == "" && idleFor >= o.idle && s.Nudges >= o.maxNudges {
		if s.Stalled {
			return ""
		}
		s.Stalled = true
		watchSave(r.ID, s)
		_ = client.SetStatus(r.WorkspaceTitle, "needs-attention")
		_ = client.Notify(r.WorkspaceTitle, r.ID+" is stalled",
			fmt.Sprintf("no pane change in %s after %d nudges", roundMin(idleFor), s.Nudges))
		return fmt.Sprintf("STALLED %s — %d nudges, no change in %s; open %q",
			r.ID, s.Nudges, roundMin(idleFor), r.WorkspaceTitle)
	}

	nudgeDue := idleFor >= o.idle && s.Nudges < o.maxNudges

	// A due status check that is not also a due nudge sends on its own; when
	// both are due the nudge below carries it — one prompt, budget still spent.
	// It also waits while an echo claim is outstanding: fingerprint-only
	// attribution cannot tell which of two prompts a pane change echoes, so at
	// most one is ever armed.
	if statusDue && !nudgeDue && !s.Pending && !s.PendingStatus {
		prompt, err := watchSend(client, r, o.nudge)
		if err != nil {
			return fmt.Sprintf("UNREACHABLE %s — %s", r.ID, err)
		}
		s.StatusCheck = now.UTC().Format(time.RFC3339)
		s.PendingStatus = true
		watchSave(r.ID, s)
		return fmt.Sprintf("STATUS %s after %s — sent %q",
			r.ID, roundMin(o.statusInterval), prompt)
	}
	if idleFor < o.idle {
		if o.once {
			return fmt.Sprintf("IDLE %s %s (nudge at %s)", r.ID, roundMin(idleFor), roundMin(o.idle))
		}
		return ""
	}

	prompt, err := watchSend(client, r, o.nudge)
	if err != nil {
		return fmt.Sprintf("UNREACHABLE %s — %s", r.ID, err)
	}
	s.Nudges++
	s.Pending = true
	// A nudge supersedes an outstanding status echo claim: the next pane
	// change is attributed to the nudge, which spends the budget — the
	// conservative call when two prompts could have produced it.
	s.PendingStatus = false
	s.Since = now.UTC().Format(time.RFC3339)
	if statusDue {
		s.StatusCheck = s.Since
	}
	watchSave(r.ID, s)
	return fmt.Sprintf("NUDGED %s after %s (%d/%d) — sent %q",
		r.ID, roundMin(idleFor), s.Nudges, o.maxNudges, prompt)
}

// watchSend delivers one prompt to the foreman's pane: Send + Enter in one
// call, because text with no Enter sits in the composer unsent while the pane
// still looks busy — which would read as a foreman ignoring the poke.
func watchSend(client *orca.Client, r foreman.Record, instruction string) (string, error) {
	agentName := r.Agent
	if agentName == "" {
		agentName = agent.Default
	}
	prof, err := agent.ByName(agentName)
	if err != nil {
		return "", err
	}
	prompt := foremanSkillPrompt(prof, r.ID, instruction)
	if err := client.SendEnter(r.WorkspaceTitle, prompt); err != nil {
		foreman.MarkUnreachable(r.ID)
		return "", err
	}
	return prompt, nil
}

// roundMin renders a duration the way an operator reads one: whole minutes,
// seconds only while it is still under a minute.
func roundMin(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
