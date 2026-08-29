package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/reallongnguyen/babysit/internal/agent"
	"github.com/reallongnguyen/babysit/internal/orca"
)

// Two independent flags, two commands:
//
//	--auto              spawn-goal on the agent that started this session
//	--reviewer <agent>       spawn-review on that agent (omit = no review);
//	                         any registered agent, and never the builder
//
// Both: review first; --builder (the start agent) makes approve run spawn-goal.
//
// A third, later in the run: --verify spawns spawn-verify once the code is
// committed, re-running review-pr and qa in a process that never saw the diff
// being written. Its only output is the verdict files it persists.

type spawnOpts struct {
	ticket, workflow, agentFlag, builder, dir string
	printOnly                                 bool
}

type spawnResult struct {
	Ticket, Workflow, Agent, Cmd, Log, Dir, Orca string
	PID                                          int
	AlreadyRunning                               bool
}

type spawnJob struct {
	kind     string // "goal" or "review" — error prefixes + refuse rules
	prompt   func(*apState, agent.Profile, string, string) string
	logName  string
	pidName  string
	extraEnv []string
	// resolve is ByName for --reviewer (the flag IS the agent) and the
	// worker ladder for spawn-goal.
	resolve func(flag string) (agent.Profile, error)
}

func parseSpawnArgs(args []string) spawnOpts {
	var o spawnOpts
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ticket":
			o.ticket, i = next(args, i)
		case "--workflow":
			o.workflow, i = next(args, i)
		case "--agent":
			o.agentFlag, i = next(args, i)
		case "--builder":
			o.builder, i = next(args, i)
		case "--dir":
			o.dir, i = next(args, i)
		case "--print":
			o.printOnly = true
		}
	}
	return o
}

func printSpawn(res spawnResult) {
	if res.AlreadyRunning {
		fmt.Printf("ALREADY_RUNNING=true\nTICKET=%s\nPID=%d\nORCA=%s\nLOG=%s\n",
			res.Ticket, res.PID, res.Orca, res.Log)
		return
	}
	if res.Orca != "" {
		fmt.Printf("SPAWNED=true\nTICKET=%s\nWORKFLOW=%s\nAGENT=%s\nORCA=%s\nDIR=%s\n",
			res.Ticket, res.Workflow, res.Agent, res.Orca, res.Dir)
		return
	}
	if res.PID == 0 {
		fmt.Println(res.Cmd)
		return
	}
	fmt.Printf("SPAWNED=true\nTICKET=%s\nWORKFLOW=%s\nAGENT=%s\nPID=%d\nLOG=%s\nDIR=%s\n",
		res.Ticket, res.Workflow, res.Agent, res.PID, res.Log, res.Dir)
}

func (a *apState) spawnGoal(args []string) {
	res, err := a.runSpawnGoal(parseSpawnArgs(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatus(err))
	}
	printSpawn(res)
}

func (a *apState) spawnReview(args []string) {
	res, err := a.runSpawnReview(parseSpawnArgs(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatus(err))
	}
	printSpawn(res)
}

func (a *apState) spawnVerify(args []string) {
	res, err := a.runSpawnVerify(parseSpawnArgs(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatus(err))
	}
	printSpawn(res)
}

func goalJob() spawnJob {
	return spawnJob{
		kind: "goal",
		prompt: func(_ *apState, prof agent.Profile, ticket, workflow string) string {
			return goalPrompt(prof, ticket, workflow)
		},
		logName: "goal.log",
		pidName: "goal.pid",
		extraEnv: []string{
			"BABYSIT_SPAWNED=true",
			"AGENT_ROLE=mayor",
			// The greenlight runs inside the reviewer, whose environment the
			// child inherits. Clear the marker so the builder is not one.
			"BABYSIT_REVIEWER=",
		},
		// --agent, then BABYSIT_BUILDER (set when --reviewer + --auto), then
		// the session that started autopilot, then the worker default.
		resolve: resolveGoalAgent,
	}
}

func resolveGoalAgent(flag string) (agent.Profile, error) {
	if flag != "" {
		return agent.ByName(flag)
	}
	if b := os.Getenv("BABYSIT_BUILDER"); b != "" {
		return agent.ByName(b)
	}
	// BABYSIT_AGENT is the documented "this run means it" override, so it
	// outranks the ambient session; Current() only stands in for an unset
	// preference, and still beats the config ladder — that is what --auto means.
	if os.Getenv("BABYSIT_AGENT") == "" {
		if cur := agent.Current(); cur != "" {
			return agent.ByName(cur)
		}
	}
	return agent.Resolve(agent.WorkerKey, "")
}

func reviewJob(builder string) spawnJob {
	extra := []string{
		"BABYSIT_REVIEWER=true",
		"AGENT_ROLE=mayor",
	}
	if builder != "" {
		extra = append(extra, "BABYSIT_BUILDER="+builder)
	}
	return spawnJob{
		kind: "review",
		prompt: func(a *apState, prof agent.Profile, ticket, workflow string) string {
			return a.reviewPrompt(prof, ticket, workflow, builder)
		},
		logName:  "review.log",
		pidName:  "review.pid",
		extraEnv: extra,
		resolve:  agent.ByName,
	}
}

// verifyJob re-runs the landing gates in a process that does not know how the
// code came to look this way. The agent is the *same* one by default, unlike
// --reviewer, which refuses the builder: that refusal guards a plan re-derived
// from the same artifacts by the same weights, where a second run adds nothing.
// The bias here is different in kind — "I wrote this line, so I know it is
// right" — and it lives in the context, not the weights. Dropping the context
// is what buys the independent read; --agent stays available for model
// diversity on top.
func verifyJob() spawnJob {
	return spawnJob{
		kind: "verify",
		prompt: func(a *apState, prof agent.Profile, ticket, _ string) string {
			return a.verifyPrompt(prof, ticket)
		},
		logName: "verify.log",
		pidName: "verify.pid",
		extraEnv: []string{
			"BABYSIT_VERIFIER=true",
			"AGENT_ROLE=mayor",
			// The verifier is a leaf: BABYSIT_SPAWNED stops it forking a
			// builder or a reviewer, BABYSIT_VERIFIER stops it forking itself,
			// and it is not the reviewer whose marker it may have inherited.
			"BABYSIT_SPAWNED=true",
			"BABYSIT_REVIEWER=",
		},
		// The same ladder the builder resolved through, so the default is the
		// agent running the build; BABYSIT_BUILDER, when a reviewer pinned one,
		// names that same agent.
		resolve: resolveGoalAgent,
	}
}

func (a *apState) runSpawnGoal(o spawnOpts) (spawnResult, error) {
	return a.runSpawn(goalJob(), o)
}

func (a *apState) runSpawnReview(o spawnOpts) (spawnResult, error) {
	if o.agentFlag == "" {
		return spawnResult{}, errSpawn{2, "spawn-review: --agent is required — one of: " +
			strings.Join(agent.Names(), ", ")}
	}
	// --builder only when --auto is also set (skill passes it). Omit it
	// and the reviewer persists the review and stops — no spawn-goal. It is
	// validated here because it reaches the greenlight only as interpolated
	// text: a typo would otherwise surface as a dead chain in a log nobody
	// reads, and a value with shell metacharacters would reach Orca's shell.
	if o.builder != "" {
		if _, err := agent.ByName(o.builder); err != nil {
			return spawnResult{}, errSpawn{2, "spawn-review: --builder " + err.Error()}
		}
		// Independence is the flag's only purpose. Spawning a second process on
		// the same model to grade the first one's plan satisfies --reviewer
		// while delivering nothing: the whole value of an agent reviewer is a
		// read the builder did not produce. Compare worker_agent / foreman_agent,
		// which deliberately do not inherit for exactly this reason.
		if o.builder == o.agentFlag {
			return spawnResult{}, errSpawn{2, fmt.Sprintf(
				"spawn-review: --reviewer %s cannot also be --builder %s — a plan reviewed by the "+
					"model that wrote it is not an independent read, which is the only thing this flag buys. "+
					"Pick a different reviewer (known agents: %s), or drop --reviewer and let the human review.",
				o.agentFlag, o.builder, strings.Join(agent.Names(), ", "))}
		}
	}
	return a.runSpawn(reviewJob(o.builder), o)
}

func (a *apState) runSpawnVerify(o spawnOpts) (spawnResult, error) {
	return a.runSpawn(verifyJob(), o)
}

// goalPrompt is the /goal block the human-handoff template in
// .claude/skills/autopilot/SKILL.md prints. Keep the condition lines in sync
// (TestGoalPromptMatchesSkillHandoff).
//
// The skill reference comes from the profile rather than being spelled
// `/bbs:autopilot` here: an agent that discovers skills through a flat
// directory list exposes them bare, and a prompt naming a prefix that agent
// does not use resolves to no skill at all — a worker that starts cleanly and
// then does nothing.
func goalPrompt(prof agent.Profile, ticket, workflow string) string {
	return "/goal " + ticket + " is done: qa verdict PASS/FIXED persisted via bbs ticket set-verdict,\n" +
		"review-pr verdict persisted, branch pushed, closed out per the repo's finish\n" +
		"policy, handoff note written — or a NEEDS_CONTEXT / BLOCKED status block\n" +
		"printed verbatim.\n" +
		"Work it: " + prof.SkillRef("autopilot") + " " + workflow + " " + ticket
}

func (a *apState) reviewPrompt(prof agent.Profile, ticket, workflow, builder string) string {
	td, _ := a.ticketDir(ticket)
	if td == "" {
		td = filepath.Join("tickets", ticket)
	}
	body := "Review the plan and prototype for " + ticket + " before any code is written.\n" +
		"\n" +
		"Read these if they exist:\n" +
		"  requirement: " + filepath.Join(td, "requirement.md") + "\n" +
		"  plan:        " + filepath.Join(td, "plan.md") + "\n" +
		"  design:      " + filepath.Join(td, "design.md") + "\n" +
		"  prototype:   " + filepath.Join(td, "prototype.html") + "\n" +
		"\n" +
		"Fill every rubric line with named evidence (a missing line is a redirect, never a pass):\n" +
		"- Coverage — each acceptance criterion maps to a named plan step / design element\n" +
		"- Host-page consistency — name the sibling screen/component\n" +
		"- Reuse — name what already exists that this uses; something new needs a stated reason\n" +
		"- Prototype inspected — actually Read the prototype; file existence is not evidence\n" +
		"- Scope — nothing beyond the request wording\n" +
		"\n" +
		"Two of those lines have no referent on a change with no UI surface — a CLI, a\n" +
		"Go package, a docs edit. Write those as `N/A — <reason>`, which counts as\n" +
		"filled: only Host-page consistency and Prototype inspected may be declined\n" +
		"that way, and a bare `N/A` with no reason does not count. Coverage, Reuse and\n" +
		"Scope always have an answer — they mean as much for a Go package as a screen.\n" +
		"\n" +
		"Persist the filled rubric:\n" +
		"  bbs ticket set-review --skill plan --body-file <review.md>\n"
	if builder != "" {
		body += "\n" +
			"Then run the gate. It is the only path to the builder — it re-reads the\n" +
			"design artifacts, checks the non-delegable floor, checks the rubric, logs\n" +
			"the decision, and starts the builder itself when all three pass:\n" +
			"  bbs autopilot review-gate --ticket " + ticket + " --workflow " + workflow +
			" --builder " + builder + " --rubric-file <review.md>\n" +
			"\n" +
			"Do not run spawn-goal yourself, and do not decide the outcome in prose —\n" +
			"the gate decides. Act on what it prints:\n" +
			"  VERDICT=APPROVE   it already started the builder; you are done\n" +
			"  VERDICT=REDIRECT  the named lines are unfilled. Re-plan against those gaps\n" +
			"                    (plan-draft), then run review-gate again. ROUNDS= says\n" +
			"                    how many you have left\n" +
			"  VERDICT=BLOCKED   rounds spent — report BLOCKED naming the unfilled lines\n" +
			"  VERDICT=ESCALATE  money/auth/irreversible-data, or a posture bound.\n" +
			"                    Report NEEDS_CONTEXT naming the path; never approve it\n"
	} else {
		body += "\nRun the gate to record the verdict, then stop — it will not start a builder\n" +
			"without --builder:\n" +
			"  bbs autopilot review-gate --ticket " + ticket + " --workflow " + workflow +
			" --rubric-file <review.md>\n"
	}
	return body + "\nPrint a STATUS block. Do not invoke " + prof.SkillRef("autopilot") + "."
}

// verifyPrompt is the fresh-context gate prompt, and what it withholds is the
// whole point. The producer's rationale — the implement handoff, and the plan's
// approach — is what makes an author read their own diff as obviously correct
// and test the path they built. So the verifier gets the two things it must
// judge against (the acceptance criteria, and the code) and is told to leave
// the reasoning unread. A prompt that pastes the handoff in here is a bug, not
// a convenience.
func (a *apState) verifyPrompt(prof agent.Profile, ticket string) string {
	td, ok := a.ticketDir(ticket)
	if !ok || td == "" {
		td = filepath.Join("tickets", ticket)
	}
	base := a.baseBranch()
	if base == "" {
		base = "main"
	}
	return "Verify " + ticket + ". You did not write this code, and you are deliberately not\n" +
		"being told why it looks the way it does. Judge it against the requirement alone.\n" +
		"\n" +
		"Read:\n" +
		"  requirement: " + filepath.Join(td, "requirement.md") + "   ← the acceptance criteria you must prove\n" +
		"  the change:  git diff $(git merge-base origin/" + base + " HEAD)\n" +
		"\n" +
		"Do NOT read " + filepath.Join(td, "handoffs") + "/ or " + filepath.Join(td, "plan.md") + ".\n" +
		"They carry the builder's reasoning, and a verifier that has read them grades the\n" +
		"reasoning instead of the code — that bias is the only thing this separate\n" +
		"process exists to remove.\n" +
		"\n" +
		"Run both gates as real skill invocations, in this order:\n" +
		"1. " + prof.SkillRef("review-pr") + " --fix over that diff.\n" +
		"2. " + prof.SkillRef("qa") + " against the acceptance criteria — at least one\n" +
		"   validation/error/empty/responsive case, not only the path the criteria describe.\n" +
		"Fix what either one finds, re-verify, and commit the fixes here in this worktree.\n" +
		"\n" +
		"Persist both verdicts. They are your only output channel — nothing you print is\n" +
		"read by the session that started you:\n" +
		"  bbs ticket set-verdict --skill review-pr --body-file <review.md>\n" +
		"  bbs ticket set-verdict --skill qa --body-file <qa.md>\n" +
		"Each body needs a `STATUS: DONE` line (or DONE_WITH_CONCERNS / BLOCKED naming the\n" +
		"blocker). Confirm with `bbs ticket verdict-status --skill qa` before you stop — an\n" +
		"unwritten verdict reads as a dead verifier, and the run stops rather than falling\n" +
		"back to the in-session QA you were spawned to replace.\n" +
		"\n" +
		"Do not push, do not open a PR, do not merge, and do not invoke " + prof.SkillRef("autopilot") + " —\n" +
		"the session that started you owns git and reads your verdicts from disk. Your\n" +
		"git-flow says the same thing (BBS_FINISH=review, BBS_PUSH=false) for as long as\n" +
		"BABYSIT_VERIFIER is set.\n" +
		"\nPrint a STATUS block.\n"
}

func (a *apState) runSpawn(job spawnJob, o spawnOpts) (spawnResult, error) {
	prefix := "spawn-" + job.kind
	if reason := refuseSpawn(job.kind); reason != "" {
		return spawnResult{}, errSpawn{2, prefix + ": " + reason}
	}

	ticket := o.ticket
	if ticket == "" {
		ticket = a.ticket
	}
	ticket = safeTicket(ticket)
	if ticket == "" {
		return spawnResult{}, errSpawn{2, prefix + ": --ticket is required (or run on a ticket branch)"}
	}

	dir := o.dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	workflow := o.workflow
	if workflow == "" {
		if td, ok := a.ticketDir(ticket); ok {
			if cf, ok := readCheckpoint(filepath.Join(td, "checkpoint.json")); ok && cf.Workflow != "" {
				workflow = cf.Workflow
			}
		}
	}
	if workflow == "" {
		workflow = "builder"
	}

	prof, err := job.resolve(o.agentFlag)
	if err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	if err := prof.Preflight(); err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	if err := prof.PreflightDir(dir); err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}

	prompt := job.prompt(a, prof, ticket, workflow)
	// Stamp the agent into the child's environment. agent.Current() cannot see
	// omp or codex — neither exports a session marker — so without this a
	// nested `--auto` inside an omp worker would fall through to the configured
	// worker_agent and quietly spawn a different CLI than the one it is running
	// in. BABYSIT_AGENT outranks Current() in resolveGoalAgent, so stamping it
	// makes the answer certain for every agent instead of just the two that
	// happen to be detectable.
	env := append([]string{"BABYSIT_AGENT=" + prof.Name}, job.extraEnv...)
	res := spawnResult{
		Ticket:   ticket,
		Workflow: workflow,
		Agent:    prof.Name,
		Cmd:      commandWithEnv(ticket, env, prof.WorkerCommand(prompt)),
		Dir:      dir,
	}
	if o.printOnly {
		return res, nil
	}

	td, ok := a.ticketDir(ticket)
	if !ok {
		return spawnResult{}, errSpawn{2, prefix + ": invalid ticket"}
	}
	if err := os.MkdirAll(td, 0o755); err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	res.Log = filepath.Join(td, job.logName)
	title := "bbs " + job.kind + " " + ticket

	// The pid guard runs before the Orca branch, not inside the fallback: a
	// worker started while Orca was down is still alive when Orca comes up,
	// and creating a tab for it would put two agents on one worktree.
	pidPath := filepath.Join(td, job.pidName)
	if pid, alive := readAlivePID(pidPath); alive {
		res.PID = pid
		res.AlreadyRunning = true
		return res, nil
	}

	if client, err := orca.Preflight(); err == nil {
		if t, err := client.LookupSpawn(job.kind, ticket); err == nil {
			res.Orca = t.Title
			res.AlreadyRunning = true
			return res, nil
		}
		if _, err := client.Create(orca.CreateOpts{Title: title, Cwd: dir, Command: res.Cmd}); err != nil {
			return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
		}
		// Prefer the live title — Orca may already have rewritten it.
		if t, err := client.LookupSpawn(job.kind, ticket); err == nil {
			title = t.Title
		}
		res.Orca = title
		_ = os.WriteFile(filepath.Join(td, job.kind+".orca"), []byte(title+"\n"), 0o644)
		return res, nil
	}

	bin, err := exec.LookPath(prof.Bin)
	if err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	argv := []string{}
	if prof.Yolo != "" {
		argv = append(argv, prof.Yolo)
	}
	argv = append(argv, prompt)

	logF, err := os.OpenFile(res.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	defer logF.Close()

	c := exec.Command(bin, argv...)
	c.Dir = dir
	c.Stdout = logF
	c.Stderr = logF
	childEnv := append(os.Environ(), "BABYSIT_TICKET="+ticket)
	childEnv = append(childEnv, env...)
	c.Env = childEnv
	c.SysProcAttr = detachedProcAttr()
	if err := c.Start(); err != nil {
		return spawnResult{}, errSpawn{1, prefix + ": " + err.Error()}
	}
	res.PID = c.Process.Pid
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(res.PID)+"\n"), 0o644)
	_ = c.Process.Release()
	return res, nil
}

func commandWithEnv(ticket string, extra []string, cmd string) string {
	parts := []string{"BABYSIT_TICKET=" + ticket}
	parts = append(parts, extra...)
	return strings.Join(parts, " ") + " " + cmd
}

// refuseSpawn stops a child from forking another of the same kind. A reviewer
// is allowed to call spawn-goal (that is the greenlight); a goal worker may
// spawn exactly one child, the verifier that grades it — a run started by
// --auto reaches its gates no other way — and nothing else.
func refuseSpawn(kind string) string {
	// The verifier is a leaf. It grades a finished diff; every fork from here
	// is either a second opinion on itself or a builder it must not become.
	if os.Getenv("BABYSIT_VERIFIER") != "" {
		return "already inside a verifier session (BABYSIT_VERIFIER is set)"
	}
	if kind != "verify" && os.Getenv("BABYSIT_SPAWNED") != "" {
		return "already inside a spawned session (BABYSIT_SPAWNED is set)"
	}
	if kind == "review" && os.Getenv("BABYSIT_REVIEWER") != "" {
		return "already inside a reviewer session (BABYSIT_REVIEWER is set)"
	}
	return ""
}

func readAlivePID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, processAlive(pid)
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

type errSpawn struct {
	code int
	msg  string
}

func (e errSpawn) Error() string { return e.msg }

func exitStatus(err error) int {
	if e, ok := err.(errSpawn); ok {
		return e.code
	}
	return 1
}
