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
//	--reviewer claude|grok   spawn-review on that agent (omit = no review)
//
// Both: review first; --builder (the start agent) makes approve run spawn-goal.

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
	prompt   func(*apState, string, string) string
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

func goalJob() spawnJob {
	return spawnJob{
		kind:    "goal",
		prompt:  func(_ *apState, ticket, workflow string) string { return goalPrompt(ticket, workflow) },
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
		prompt: func(a *apState, ticket, workflow string) string {
			return a.reviewPrompt(ticket, workflow, builder)
		},
		logName:  "review.log",
		pidName:  "review.pid",
		extraEnv: extra,
		resolve:  agent.ByName,
	}
}

func (a *apState) runSpawnGoal(o spawnOpts) (spawnResult, error) {
	return a.runSpawn(goalJob(), o)
}

func (a *apState) runSpawnReview(o spawnOpts) (spawnResult, error) {
	if o.agentFlag == "" {
		return spawnResult{}, errSpawn{2, "spawn-review: --agent claude|grok is required"}
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
	}
	return a.runSpawn(reviewJob(o.builder), o)
}

// goalPrompt is the /goal block the human-handoff template in
// .claude/skills/autopilot/SKILL.md prints. Keep the condition lines in sync
// (TestGoalPromptMatchesSkillHandoff).
func goalPrompt(ticket, workflow string) string {
	return "/goal " + ticket + " is done: qa verdict PASS/FIXED persisted via bbs ticket set-verdict,\n" +
		"review-pr verdict persisted, branch pushed, handoff note written — or a\n" +
		"NEEDS_CONTEXT / BLOCKED status block printed verbatim.\n" +
		"Work it: /bbs:autopilot " + workflow + " " + ticket
}

func (a *apState) reviewPrompt(ticket, workflow, builder string) string {
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
		"- Host-page consistency — name the sibling screen/component (or N/A with why)\n" +
		"- Reuse — name existing components; a new one needs a stated reason\n" +
		"- Prototype inspected — actually Read the prototype; file existence is not evidence\n" +
		"- Scope — nothing beyond the request wording\n" +
		"\n" +
		"Persist the filled rubric:\n" +
		"  bbs ticket set-review --skill plan --body-file <review.md>\n"
	if builder != "" {
		body += "\n" +
			"Then:\n" +
			"- APPROVE (every line filled, no money/auth/irreversible-data path): run\n" +
			"    bbs autopilot spawn-goal --ticket " + ticket + " --workflow " + workflow + " --agent " + builder + "\n" +
			"- REDIRECT or BLOCKED: write the gaps into the review; do not spawn-goal.\n"
	} else {
		body += "\nDo not spawn-goal. Persist the review and stop.\n"
	}
	return body + "\nPrint a STATUS block. Do not invoke /bbs:autopilot."
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

	prompt := job.prompt(a, ticket, workflow)
	res := spawnResult{
		Ticket:   ticket,
		Workflow: workflow,
		Agent:    prof.Name,
		Cmd:      commandWithEnv(ticket, job.extraEnv, prof.WorkerCommand(prompt)),
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
	env := append(os.Environ(), "BABYSIT_TICKET="+ticket)
	env = append(env, job.extraEnv...)
	c.Env = env
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
// is allowed to call spawn-goal (that is the greenlight); a goal worker is
// not allowed to spawn either kind.
func refuseSpawn(kind string) string {
	if os.Getenv("BABYSIT_SPAWNED") != "" {
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
