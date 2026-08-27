package orca

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Orca's inter-agent message bus, adopted as the foreman's worker mailbox.
//
// The division of labour is the whole point of adopting only this much: Orca
// owns the message bus; babysit keeps owning placement, argv, git isolation and
// tab lifecycle. So this file wraps run / task / dispatch / check / reply / ask
// and nothing else. Deliberately NOT wrapped, because babysit already owns each
// of them and better for its problem:
//
//   - worker-start / --agent — wants Orca's opaque agent-id registry and
//     rejects custom argv. babysit launches from its own agent registry.
//   - --worktree new-* — babysit owns git isolation (--mode=worktree,
//     merge-base, qa-lease).
//   - gate-create / gate-resolve — duplicates `bbs ticket approval`, which is
//     richer (rubric, non-delegable floor, decisions.jsonl, dashboard UI). Two
//     approval mechanisms means two places for the floor to be wrong.
//   - task DAGs / --deps — foreman batches are independent tickets by
//     construction.
//   - the worker lifecycle (worker-stop / -release / -abandon) — foreman
//     archives and closes tabs on its own terms, and on the non-injecting
//     dispatch path these are no-ops anyway.
//
// Dispatch is always non-injecting. `--inject` would hand Orca's lifecycle the
// job of delivering the prompt, which is exactly the half babysit keeps: a
// non-injecting dispatch is unsupervised in Orca's lifecycle sense, so
// worker-stop / worker-release will never reach into babysit's tabs.

// CapOrchestration is the capability an Orca runtime advertises when it serves
// this contract. It is checked rather than assumed because the surface is
// version-drifting and foreman must stay usable on an older Orca — the caller
// falls back to the pane monitor when this is absent.
const CapOrchestration = "orchestration.contract.v1"

// ErrNoOrchestration means this runtime does not advertise CapOrchestration.
// It is a distinct error because its handler is distinct: not a failure to
// report, but a signal to take the fallback path.
var ErrNoOrchestration = errors.New("orca runtime does not serve " + CapOrchestration)

// Supports reports whether the runtime advertised a capability at Preflight.
// The list is read once, at status time, rather than per call: it changes only
// when the app restarts, and a probe per call would put an RPC in front of
// every mailbox read.
func (c *Client) Supports(capability string) bool {
	for _, have := range c.caps {
		if have == capability {
			return true
		}
	}
	return false
}

// Orchestration reports whether the durable mailbox is available at all. The
// one call a caller should gate on before using anything else in this file.
func (c *Client) Orchestration() bool { return c.Supports(CapOrchestration) }

// Message is one item off the bus. It is the typed replacement for grepping an
// ANSI terminal tail: the fields below are what the pane monitor had to infer
// from text, and could lose entirely when a line scrolled past a 60-line read.
//
// Type is the routing key — worker_done, escalation, question, handoff. Payload
// fields (TaskID, DispatchID, Outcome, FilesModified) arrive as a JSON string
// inside the envelope and are flattened here so a caller never parses twice.
type Message struct {
	ID       string
	Type     string
	Subject  string
	Body     string
	From     string
	ThreadID string
	Sequence int

	TaskID        string
	DispatchID    string
	Outcome       string
	FilesModified []string
}

// Done reports a worker that finished, successfully or not. It is the doorbell
// only: babysit's rule is unchanged, and the on-disk verdict
// (`bbs ticket verdict-status`) stays the truth. A worker that says it passed
// and did not write a verdict has not passed.
func (m Message) Done() bool { return m.Type == "worker_done" }

// NeedsAnswer reports a message the foreman must reply to before the sender can
// continue. A worker that called `orca orchestration ask` is parked until Reply
// lands — and it arrives here as a `question`: `ask` is the command, never a
// message type, and the runtime rejects the name outright.
func (m Message) NeedsAnswer() bool {
	return m.Type == "question" || m.Type == "escalation"
}

// RunCreate binds a FRESH Run to this terminal and returns its id.
//
// Fresh per foreman session, never adopting an existing one: the contract
// carries a large legacy-authority surface (legacy labels, run-use
// --takeover-legacy) whose migration rules babysit has no reason to inherit.
// A new run is a clean namespace with this terminal as coordinator.
func (c *Client) RunCreate(objective string) (string, error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	raw, err := c.run("orchestration", "run-create", "--objective", objective)
	if err != nil {
		return "", err
	}
	return runIDFrom(raw)
}

// RunUse rebinds this terminal as the coordinator of an existing Run.
//
// Coordinator binding is per-terminal, so a foreman that resumes in a new
// terminal — the normal case after a crash or a closed tab — has to rebind or
// its whole mailbox reads as somebody else's. --takeover-legacy is never
// passed: it is fenced against the original coordinator and only means
// anything for runs babysit did not create.
func (c *Client) RunUse(id string) error {
	if !c.Orchestration() {
		return ErrNoOrchestration
	}
	_, err := c.run("orchestration", "run-use", "--id", id)
	return err
}

// RunCurrent returns the Run bound to this terminal, or "" if none is.
func (c *Client) RunCurrent() (string, error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	raw, err := c.run("orchestration", "run-current")
	if err != nil {
		return "", err
	}
	id, err := runIDFrom(raw)
	if err != nil {
		return "", nil // bound to nothing is not a failure
	}
	return id, nil
}

// TaskCreate makes the task a worker's messages hang off. One per ticket:
// deps are never passed, because a foreman batch is independent tickets by
// construction and a DAG would model a dependency that does not exist.
func (c *Client) TaskCreate(runID, title, spec string) (string, error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	args := []string{"orchestration", "task-create", "--spec", spec, "--task-title", title, "--display-name", title}
	if runID != "" {
		args = append(args, "--run", runID)
	}
	raw, err := c.run(args...)
	if err != nil {
		return "", err
	}
	var wrap struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &wrap)
	if wrap.Task.ID != "" {
		return wrap.Task.ID, nil
	}
	if wrap.ID != "" {
		return wrap.ID, nil
	}
	return "", fmt.Errorf("orca task-create: no task id in response")
}

// Dispatch binds a task to a terminal. It NEVER passes --inject, and it never
// asks for Orca's lifecycle preamble.
//
// The preamble was a dead end. It names the task id a worker needs to report
// worker_done, but it can only be fetched once the terminal exists — by which
// point babysit has already delivered the prompt, so there is nothing left to
// prepend it to. The worker joins the same ids itself (Self + TaskFor), which
// is one fewer thing to deliver and one fewer way to deliver it wrong.
func (c *Client) Dispatch(runID, taskID, toHandle string) error {
	if !c.Orchestration() {
		return ErrNoOrchestration
	}
	args := []string{"orchestration", "dispatch", "--task", taskID, "--to", toHandle}
	if runID != "" {
		args = append(args, "--run", runID)
	}
	_, err := c.run(args...)
	return err
}

// Self reports the Run this terminal is working under, read from the worker's
// own side of the bus. An empty run is the honest answer for a terminal bound
// to no Run — task-list then falls back to whatever binding orca resolves.
//
// --peek is load-bearing: a plain check marks the batch read, so a worker
// ringing the doorbell on its way out would swallow whatever the coordinator
// had queued for it.
func (c *Client) Self() (runID string, err error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	raw, err := c.run("orchestration", "check", "--peek")
	if err != nil {
		return "", err
	}
	var self struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(raw, &self); err != nil {
		return "", fmt.Errorf("orca check: %w", err)
	}
	return self.RunID, nil
}

// TaskFor finds the task a ticket was dispatched as, by the title foreman gave
// it (TaskCreate sets --task-title and --display-name to the ticket id).
//
// The ticket is the join key because it is the only one both halves of the bus
// agree on: a task row carries `task_title`, and the worker carries
// BABYSIT_TICKET. Orca's task rows carry no dispatch id — a dispatch is a
// separate record keyed the other way, by task — so a worker cannot start from
// "which dispatch am I?" and arrive anywhere.
//
// A ticket re-dispatched inside one run has more than one row; the live one
// wins, and the newest row otherwise.
func (c *Client) TaskFor(runID, ticket string) (string, error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	if ticket == "" {
		return "", errors.New("orca task-list: needs a ticket")
	}
	args := []string{"orchestration", "task-list", "--brief"}
	if runID != "" {
		args = append(args, "--run", runID)
	}
	raw, err := c.run(args...)
	if err != nil {
		return "", err
	}
	var list struct {
		Tasks []struct {
			ID     string `json:"id"`
			Title  string `json:"task_title"`
			Name   string `json:"display_name"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", fmt.Errorf("orca task-list: %w", err)
	}
	var newest, live string
	for _, t := range list.Tasks {
		if t.Title != ticket && t.Name != ticket {
			continue
		}
		newest = t.ID
		if t.Status != "completed" && t.Status != "failed" {
			live = t.ID
		}
	}
	if live != "" {
		return live, nil
	}
	if newest != "" {
		return newest, nil
	}
	return "", fmt.Errorf("orca task-list: no task titled %s", ticket)
}

// DispatchFor reads the dispatch a task is currently assigned as. Orca marks
// the dispatch completed alongside the task when worker_done carries both ids,
// so it is worth one extra call — but not worth failing over: a worker_done
// with only the task id still lands, matched on the sender handle.
func (c *Client) DispatchFor(taskID string) (string, error) {
	if !c.Orchestration() {
		return "", ErrNoOrchestration
	}
	if taskID == "" {
		return "", errors.New("orca dispatch-show: needs a task")
	}
	raw, err := c.run("orchestration", "dispatch-show", "--task", taskID)
	if err != nil {
		return "", err
	}
	var wrap struct {
		Dispatch struct {
			ID string `json:"id"`
		} `json:"dispatch"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return "", fmt.Errorf("orca dispatch-show: %w", err)
	}
	return wrap.Dispatch.ID, nil
}

// DoneOpts is the doorbell a worker rings on its way out.
type DoneOpts struct {
	Run, Task, Dispatch string
	// From is the worker's own terminal handle. Orca falls back to matching the
	// sender against the dispatch assignee when pane identity is unstable, so
	// passing it is cheap insurance against a rejected report.
	From    string
	Subject string
	Body    string
	// Outcome is Orca's vocabulary — succeeded or failed, nothing else.
	Outcome string
	Files   []string
}

// WorkerDone reports a finished dispatch. It is the doorbell and not the
// verdict: the coordinator still reads `bbs ticket verdict-status` off disk
// before believing any of it.
func (c *Client) WorkerDone(o DoneOpts) error {
	if !c.Orchestration() {
		return ErrNoOrchestration
	}
	args := []string{"orchestration", "send", "--type", "worker_done",
		"--subject", o.Subject, "--body", o.Body,
		"--task-id", o.Task, "--outcome", o.Outcome}
	if o.Dispatch != "" {
		args = append(args, "--dispatch-id", o.Dispatch)
	}
	if o.Run != "" {
		args = append(args, "--run", o.Run)
	}
	if o.From != "" {
		args = append(args, "--from", o.From)
	}
	if len(o.Files) > 0 {
		args = append(args, "--files-modified", strings.Join(o.Files, ","))
	}
	_, err := c.run(args...)
	return err
}

// CheckOpts describes one mailbox read.
type CheckOpts struct {
	Run string
	// Types filters to the message kinds the foreman acts on. Empty means all.
	Types []string
	// WaitMS > 0 blocks until a matching message arrives or the timeout
	// expires. This is the call that replaces N per-worker poll loops with one
	// blocking read for the whole batch.
	WaitMS int
	// Ack is the DeliveryID of the previous batch, acknowledged before this
	// read. Delivery is FIFO and replays until acknowledged, which is the
	// property the pane monitor did not have: an event that scrolled past a
	// 60-line tail was simply gone.
	//
	// It is an id and not a bool because `--ack` takes one. A bare `--ack`
	// is accepted by the CLI and silently acknowledges nothing — the response
	// comes back `acknowledged: null, replayed: true` — so a caller that
	// thought it was acking gets the same batch again on every read, forever,
	// acting on the same worker_done each time. That failure is invisible
	// until you look at the field, which is why Batch carries it.
	Ack string
}

// Batch is one mailbox read: the messages, plus the handle needed to
// acknowledge them. The two travel together because acknowledging is a
// separate later call — a foreman acts on a batch, then acks it on its NEXT
// read — and a delivery id that got dropped in between is a batch that
// replays.
type Batch struct {
	DeliveryID string
	Messages   []Message
}

// Check reads the mailbox. A wait that expires with nothing is not an error —
// it returns no messages, and the caller loops.
func (c *Client) Check(o CheckOpts) (Batch, error) {
	if !c.Orchestration() {
		return Batch{}, ErrNoOrchestration
	}
	args := []string{"orchestration", "check"}
	if o.Run != "" {
		args = append(args, "--run", o.Run)
	}
	if o.Ack != "" {
		args = append(args, "--ack", o.Ack)
	}
	if len(o.Types) > 0 {
		args = append(args, "--types", strings.Join(o.Types, ","))
	}
	if o.WaitMS > 0 {
		args = append(args, "--wait", "--timeout-ms", strconv.Itoa(o.WaitMS))
	}
	raw, err := c.run(args...)
	if err != nil {
		return Batch{}, err
	}
	var wrap struct {
		DeliveryID string `json:"deliveryId"`
		Messages   []struct {
			ID       string      `json:"id"`
			Type     string      `json:"type"`
			Subject  string      `json:"subject"`
			Body     string      `json:"body"`
			From     string      `json:"from_handle"`
			ThreadID string      `json:"thread_id"`
			Sequence int         `json:"sequence"`
			Payload  interface{} `json:"payload"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return Batch{}, fmt.Errorf("orchestration check: %w", err)
	}
	out := make([]Message, 0, len(wrap.Messages))
	for _, m := range wrap.Messages {
		msg := Message{
			ID: m.ID, Type: m.Type, Subject: m.Subject, Body: m.Body,
			From: m.From, ThreadID: m.ThreadID, Sequence: m.Sequence,
		}
		applyPayload(&msg, m.Payload)
		out = append(out, msg)
	}
	return Batch{DeliveryID: wrap.DeliveryID, Messages: out}, nil
}

// Reply answers one message, unblocking a worker parked on `ask`.
//
// It is addressed by message id and nothing else. That is the property worth
// the whole adoption: the pane path answered a question by typing words into a
// composer and hoping the TUI submitted them, which is a failure class — not a
// failure — that this removes entirely.
func (c *Client) Reply(runID, msgID, body string) error {
	if !c.Orchestration() {
		return ErrNoOrchestration
	}
	args := []string{"orchestration", "reply", "--id", msgID, "--body", body}
	if runID != "" {
		args = append(args, "--run", runID)
	}
	_, err := c.run(args...)
	return err
}

// AskOpts is a worker-side question that blocks until the coordinator answers.
type AskOpts struct {
	Run      string
	Question string
	// Options are the choices, when the question has a fixed set.
	Options []string
	// TimeoutMS bounds the block. A timeout leaves the question PENDING; the
	// caller resumes it by id and must never re-ask — a second question is a
	// second thread, and the human sees a duplicate they cannot reconcile.
	TimeoutMS int
	// Resume re-opens a question that timed out, by its message id.
	Resume string
}

// AskResult is the answer, or the id to resume by when the wait expired.
type AskResult struct {
	// MessageID identifies the question thread. Keep it: it is the only way to
	// resume without asking twice.
	MessageID string
	// Answer is the coordinator's reply; empty when Pending.
	Answer string
	// Pending means the timeout expired with the question still unanswered.
	Pending bool
}

// Ask puts a User Challenge on the bus and blocks. This is the worker half of
// the AGENT_ROLE=orca escalation channel.
func (c *Client) Ask(o AskOpts) (AskResult, error) {
	if !c.Orchestration() {
		return AskResult{}, ErrNoOrchestration
	}
	args := []string{"orchestration", "ask"}
	switch {
	case o.Resume != "":
		args = append(args, "--resume", o.Resume)
	case o.Question != "":
		args = append(args, "--question", o.Question)
	default:
		return AskResult{}, errors.New("orchestration ask: needs a question or a message id to resume")
	}
	if o.Run != "" {
		args = append(args, "--run", o.Run)
	}
	if len(o.Options) > 0 {
		args = append(args, "--options", strings.Join(o.Options, ","))
	}
	if o.TimeoutMS > 0 {
		args = append(args, "--timeout-ms", strconv.Itoa(o.TimeoutMS))
	}
	raw, err := c.run(args...)
	if err != nil {
		return AskResult{}, err
	}
	var wrap struct {
		MessageID string `json:"messageId"`
		ID        string `json:"id"`
		Answer    string `json:"answer"`
		Body      string `json:"body"`
		Status    string `json:"status"`
		Pending   bool   `json:"pending"`
	}
	_ = json.Unmarshal(raw, &wrap)
	res := AskResult{MessageID: wrap.MessageID, Answer: wrap.Answer}
	if res.MessageID == "" {
		res.MessageID = wrap.ID
	}
	if res.Answer == "" {
		res.Answer = wrap.Body
	}
	res.Pending = wrap.Pending || wrap.Status == "pending" || res.Answer == ""
	return res, nil
}

// applyPayload flattens the orchestration payload onto the message. Orca sends
// it as a JSON *string* inside the envelope, so it is decoded twice — and a
// payload that fails to decode is dropped rather than fatal: the envelope
// fields are still worth acting on, and the caller checks disk for the truth
// either way.
func applyPayload(m *Message, raw interface{}) {
	var body []byte
	switch v := raw.(type) {
	case string:
		body = []byte(v)
	case map[string]interface{}, []interface{}:
		body, _ = json.Marshal(v)
	default:
		return
	}
	var p struct {
		TaskID        string   `json:"taskId"`
		DispatchID    string   `json:"dispatchId"`
		Outcome       string   `json:"outcome"`
		FilesModified []string `json:"filesModified"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return
	}
	m.TaskID, m.DispatchID = p.TaskID, p.DispatchID
	m.Outcome, m.FilesModified = p.Outcome, p.FilesModified
}

func runIDFrom(raw json.RawMessage) (string, error) {
	var wrap struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
		RunID string `json:"runId"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(raw, &wrap)
	for _, id := range []string{wrap.Run.ID, wrap.RunID, wrap.ID} {
		if id != "" {
			return id, nil
		}
	}
	return "", errors.New("orca: no run id in response")
}
