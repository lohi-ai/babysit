package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/spf13/cobra"
)

// newTicketCmd is the full Go port of the former bin/bbs-ticket.bash as
// `bbs ticket`; the bin/bbs-ticket compat symlink dispatches on argv[0].
//
// Every subcommand now runs natively: the identity core (resolve/verdicts/
// session/board), the index.json state-accessors (env/get/set-status/set-phase/
// set-parent/add-child/add-relation/set-sibling/add-label/set-pointer/
// get-pointer/ensure-size/append-history), the file-only manifest.yaml ops
// (init/get-manifest/set-branch), the git-mutating base-ops family (merge-base/
// refresh/reset-base/switch/serve/qa-lease), `ensure`, and path/list/reconcile/
// find-similar. A byte-identical frozen copy of the retired script survives at
// tests/fixtures/bbs-ticket.reference as the differential-harness oracle.
//
// Flag parsing is disabled and each subcommand hand-parses its argv, because
// the bash original hand-parses too and its quirks are part of the contract
// (resolve silently ignores unknown args; set-verdict rejects them with exit 2).
func newTicketCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "ticket",
		Short:              "ticket identity (resolve/board/verdicts/sessions)",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				ticketUsage()
				os.Exit(2)
			}
			switch args[0] {
			case "-h", "--help", "help":
				ticketUsage()
				os.Exit(0)
			case "resolve":
				runResolve(args[1:])
			case "verdict-status":
				runVerdictStatus(args[1:])
			case "set-verdict":
				runSetVerdict(args[1:])
			case "session":
				runSession(args[1:])
			case "board":
				runBoard(args[1:])
			case "env":
				runTicketEnv()
			case "get":
				runGet(args[1:])
			case "set-status":
				runSetStatus(args[1:])
			case "set-phase":
				runSetPhase(args[1:])
			case "set-parent":
				runSetParent(args[1:])
			case "assign":
				runAssign(args[1:])
			case "pause":
				runPause(args[1:])
			case "cancel":
				runCancel(args[1:])
			case "resume":
				runResume()
			case "restore":
				runRestore()
			case "add-child":
				runAddChild(args[1:])
			case "add-relation":
				runAddRelation(args[1:])
			case "set-sibling":
				runSetSibling(args[1:])
			case "add-label":
				runAddLabel(args[1:])
			case "set-pointer":
				runSetPointer(args[1:])
			case "get-pointer":
				runGetPointer(args[1:])
			case "ensure-size":
				runEnsureSize()
			case "append-history":
				runAppendHistory(args[1:])
			case "init":
				runInit(args[1:])
			case "get-manifest":
				runGetManifest(args[1:])
			case "set-branch":
				runSetBranch(args[1:])
			case "ensure":
				runEnsure(args[1:])
			case "merge-base":
				runMergeBase(args[1:])
			case "refresh":
				runRefresh(args[1:])
			case "reset-base":
				runResetBase(args[1:])
			case "switch":
				runSwitch(args[1:])
			case "serve":
				runServe(args[1:])
			case "qa-lease":
				runQALease(args[1:])
			case "add-handoff":
				runAddHandoff(args[1:])
			case "latest-handoff":
				runLatestHandoff(args[1:])
			case "set-review":
				runSetReview(args[1:])
			case "set-evidence":
				runSetEvidence(args[1:])
			case "evidence-status":
				runEvidenceStatus(args[1:])
			case "qa-evidence":
				runQAEvidence(args[1:])
			case "path":
				runPath(args[1:])
			case "list":
				runList(args[1:])
			case "reconcile":
				runReconcile(args[1:])
			case "find-similar":
				runFindSimilar(args[1:])
			case "assert-cwd":
				runAssertCwd()
			default:
				fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", args[0])
				os.Exit(2)
			}
			return nil
		},
	}
}

// ticketUsage prints the subcommand listing (bbs-ticket.bash:170-237) to stderr.
// help/-h/--help exit 0 after this; empty and unknown subcommands exit 2.
func ticketUsage() {
	fmt.Fprint(os.Stderr, ticketUsageText)
}

const ticketUsageText = `usage: bbs-ticket <subcommand> [args...]

Subcommands:
  ensure            idempotent: no-op on ticket branch, else cut one + init
                    mode trunk|branch|worktree (--mode > git-flow.yaml mode:):
                    trunk = no cut; branch = in-place from clean base else
                    worktree divert; worktree = always divert, primary stays
                    on base; diverts print WORKTREE=<path>; developer role
                    asks before in-place — exit 3; --cut-branch/--no-branch
  init              initialize index.json + manifest.yaml for the current ticket
  resolve [--explain]   print ticket id (env → manifest cwd-match → branch);
                        exit 0 resolved, 1 no resolution, 2 conflict-BLOCKED
  get-manifest <ticket> emit manifest.yaml as JSON
  set-branch <ticket> <repo> <branch>
                        rewrite manifest.yaml — update branch on the named repo row
  get <path>        print a field (dotted path)
  set-status <s>    set ticket status
  set-phase <s>     set current owning skill
  set-parent <t>    set parent ticket
  assign <foreman-id>|--none    set the owning foreman (assignee)
  pause [--note M]  human override: stop dispatch, keep status (reversible)
  cancel [--note M] human override: drop from dispatch, keep status (reversible)
  resume            clear a pause      restore   clear a cancel
  add-child <t>     append a child ticket id
  add-relation <type> <target>
  set-sibling --role R --repo REPO --ticket T
  add-label <label>
  set-pointer <key> <value>
  get-pointer <key>             print pointers.<key> ("" if unset)
  ensure-size                   resolve ticket_size (XS|S|M|L); estimate from diff if unset
  add-handoff --skill S --status STATUS [--body MD | --body-file FILE]
  latest-handoff [--skill S]    print latest handoff filename (optionally filtered by skill)
  set-verdict --skill S [--body MD | --body-file FILE]
  verdict-status --skill S       print {none|DONE|DONE_WITH_CONCERNS|BLOCKED|NEEDS_CONTEXT}
  set-review  --skill S [--body MD | --body-file FILE]
  set-evidence --kind K (--json STR | --json-file FILE)   K ∈ verification|risk-gate|adversarial
  evidence-status --kind K       print {none|valid|malformed}
  qa-evidence                    audit qa verdict body: {none|ok|contradiction:<d>|thin:<d>|unexplained}
  append-history --event E [--actor A] [--extra-json JSON]
  merge-base [--base BRANCH]     from a ticket worktree: merge the ticket branch
                                 into the primary checkout (dev-server tree);
                                 BLOCKs on dirty/diverged state or conflict
  refresh [--base BRANCH]        bring the ticket branch up to date: fetch +
                                 merge origin/<base> into it (never local base);
                                 BLOCKs on dirty tree or conflict
  reset-base [--base BRANCH]     reset the primary checkout's base branch to
                                 origin/<base> (drops local integration merges);
                                 BLOCKs on dirty/off-base/non-merge local commits
  switch <ticket>... [--base BRANCH]
                                 test surface = base + exactly these tickets:
                                 reset-base then merge each ticket branch in;
                                 the fast QA hop between worktree tickets
  qa-lease <acquire|release|status> [--ticket ID] [--ttl-min N] [--force]
                                 exclusive QA-session lease on the test surface;
                                 other tickets' merge-base/switch/reset-base
                                 BLOCK while held; stale (> ttl) is stolen
  serve [<ticket>...] [--ttl-min N]
                                 human review: long qa-lease (240min) + switch,
                                 here and in each sibling repo; bare = every
                                 finished ticket (qa + review-pr DONE); re-run
                                 after each fix; serve --release frees leases
  board [--all] [--pr]           read-only ticket board: status, branch, qa/
                                 review verdicts, session, PR, qa-lease, serving
  path <kind> [selectors] --read|--write   resolve a ticket file path (canonical → legacy)
  list <kind> [selectors]                  list ticket files of a kind
  reconcile [--ticket <id> | --all] [--dry-run] [--quiet]
                    advance index.json.status from observable filesystem state
  session <list|attach|end>      inspect/rehydrate ~/.babysit/sessions/
  env               print TICKET / TICKET_HOME / INDEX for eval
`

// ─── shared helpers ──────────────────────────────────────────────────────

// needTicket mirrors bash need_ticket() (bbs-ticket.bash:262-266).
func needTicket(env identity.Env) {
	if env.Ticket == "" {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket: no ticket in scope (branch='%s'; set BBS_TICKET to override)\n"), env.Branch)
		os.Exit(2)
	}
}

// safePathComponent mirrors bash _safe_path_component() (bbs-ticket.bash:690-708)
// with _PATH_KIND=verdict: traversal exits 3, other validation exits 2.
func safePathComponent(kind, label, value string) string {
	if value == "" {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: --%s is empty (try: bbs-ticket path)\n"), kind, label)
		os.Exit(2)
	}
	if strings.Contains(value, "/") || strings.Contains(value, "..") || strings.HasPrefix(value, "/") {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: --%s '%s' rejected (path traversal)\n"), kind, label, value)
		os.Exit(3)
	}
	cleaned := keepChars(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
	if cleaned != value {
		fmt.Fprintf(os.Stderr, retarget("bbs-ticket path: %s: --%s '%s' contains forbidden characters (allowed: a-zA-Z0-9._-)\n"), kind, label, value)
		os.Exit(2)
	}
	return value
}

func keepChars(s, allowed string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(allowed, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
