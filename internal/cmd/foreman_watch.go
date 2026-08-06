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

	"github.com/reallongnguyen/babysit/internal/cmux"
	"github.com/reallongnguyen/babysit/internal/foreman"
	"github.com/reallongnguyen/babysit/internal/identity"
)

// Watchdog for a foreman that stopped moving.
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
	// Stalled records that the budget ran out and was reported, so the loop says
	// it once rather than every interval.
	Stalled bool `json:"stalled,omitempty"`
}

type watchOpts struct {
	interval  time.Duration
	idle      time.Duration
	lines     int
	nudge     string
	maxNudges int
	once      bool
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
		interval:  60 * time.Second,
		idle:      10 * time.Minute,
		lines:     40,
		nudge:     "check status",
		maxNudges: 3,
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
	client, err := cmux.Preflight()
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
			// foreman finished and its workspace closed, which is the outcome
			// the batch was aiming for.
			fmt.Println("watch: no foreman with an open cmux workspace — nothing to watch")
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
// whose workspace cmux still has open.
//
// "Open workspace", not Live() — a foreman's heartbeat is written by the
// foreman, so a session wedged long enough to need a nudge is exactly the one
// that has gone stale. Selecting on liveness would drop every foreman this
// command exists to catch.
func watchTargets(client *cmux.Client, id string) ([]foreman.Record, error) {
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
func watchTick(client *cmux.Client, r foreman.Record, o watchOpts, now time.Time) string {
	pane, err := client.CapturePane(r.WorkspaceTitle, o.lines)
	if err != nil {
		if errors.Is(err, cmux.ErrNoWorkspace) {
			return fmt.Sprintf("GONE %s — workspace %q is closed", r.ID, r.WorkspaceTitle)
		}
		foreman.MarkUnreachable(r.ID)
		return fmt.Sprintf("UNREACHABLE %s — %s", r.ID, err)
	}
	foreman.ClearUnreachable(r.ID)

	fp := paneFingerprint(pane)
	s := watchLoad(r.ID)
	if s.Fingerprint != fp {
		moved := "MOVING"
		if s.Pending {
			// The pane changed for the first time since we nudged, so this is
			// most likely our own text echoing. Restart the idle clock but keep
			// the nudge budget spent: real work changes the pane on more than
			// one tick, and the tick after this one is what clears the counter.
			s.Pending = false
			moved = "NUDGE-ECHO"
		} else {
			s.Nudges, s.Stalled = 0, false
		}
		s.Fingerprint, s.Since = fp, now.UTC().Format(time.RFC3339)
		watchSave(r.ID, s)
		if o.once {
			return fmt.Sprintf("%s %s", moved, r.ID)
		}
		return ""
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
	if idleFor < o.idle {
		if o.once {
			return fmt.Sprintf("IDLE %s %s (nudge at %s)", r.ID, roundMin(idleFor), roundMin(o.idle))
		}
		return ""
	}
	if s.Nudges >= o.maxNudges {
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

	// Send + Enter: cmux Send is a pure text channel, so text with no keypress
	// behind it sits in the composer unsent while the pane still looks busy —
	// which would read as a foreman ignoring the nudge.
	if err := client.Send(r.WorkspaceTitle, o.nudge); err != nil {
		foreman.MarkUnreachable(r.ID)
		return fmt.Sprintf("UNREACHABLE %s — %s", r.ID, err)
	}
	if err := client.SendKey(r.WorkspaceTitle, "enter"); err != nil {
		return fmt.Sprintf("UNREACHABLE %s — nudge typed but not submitted: %s", r.ID, err)
	}
	s.Nudges++
	s.Pending = true
	s.Since = now.UTC().Format(time.RFC3339)
	watchSave(r.ID, s)
	return fmt.Sprintf("NUDGED %s after %s (%d/%d) — sent %q",
		r.ID, roundMin(idleFor), s.Nudges, o.maxNudges, o.nudge)
}

// roundMin renders a duration the way an operator reads one: whole minutes,
// seconds only while it is still under a minute.
func roundMin(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
