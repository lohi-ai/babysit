package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/reallongnguyen/babysit/internal/qaconfig"
	"github.com/spf13/cobra"
)

// qaConfigUsage must stay byte-identical to the heredoc in bin/bbs-qa-config.
const qaConfigUsage = `Usage:
  bbs-qa-config probe --env <name> [--repo <name>]
  bbs-qa-config list
  bbs-qa-config default-env
  bbs-qa-config check
  bbs-qa-config leak-check <file>
`

// newQAConfigCmd ports bin/bbs-qa-config as `bbs qa-config`, matching its
// output bytes and exit codes exactly. Flag parsing is disabled: the bash's
// arg loop (including its silent exit-1 on a dangling --env/--repo under
// set -e) is part of the contract.
func newQAConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "qa-config",
		Short:              "read named-environment QA config from project files",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runQAConfig(args)
		},
	}
}

func runQAConfig(args []string) error {
	sub := "help"
	if len(args) > 0 {
		if args[0] != "" { // SUB="${1:-help}" defaults on empty too
			sub = args[0]
		}
		args = args[1:]
	}
	switch sub {
	case "probe":
		return runQAConfigProbe(args)
	case "list":
		return runQAConfigList()
	case "default-env":
		return runQAConfigDefaultEnv()
	case "check":
		return runQAConfigCheck()
	case "leak-check":
		file := ""
		if len(args) > 0 {
			file = args[0]
		}
		return qaConfigLeakCheckExit(qaConfigLeakCheck(file))
	default: // help|*) — any unknown subcommand prints usage, exit 0
		fmt.Print(qaConfigUsage)
		return nil
	}
}

// qaConfigLeakCheckExit maps a leak-check status to the process exit code the
// bash produces via set -e (0 stays in-process, 1 via errSilent, 2 directly).
func qaConfigLeakCheckExit(status int) error {
	switch status {
	case 0:
		return nil
	case 2:
		os.Exit(2)
	}
	return errSilent
}

func runQAConfigProbe(args []string) error {
	envName := ""
	for i := 0; i < len(args); {
		switch args[i] {
		case "--env", "--repo": // --repo reserved; parsed and ignored
			if i+1 >= len(args) {
				// `shift 2` with one arg left fails under set -e: silent exit 1.
				return errSilent
			}
			if args[i] == "--env" {
				envName = args[i+1]
			}
			i += 2
		default:
			i++
		}
	}

	if envName == "" {
		fmt.Fprintln(os.Stderr, "bbs-qa-config probe: --env <name> required")
		os.Exit(2)
	}

	// Resolve fields with last-write-wins precedence. Each field resolves
	// independently across sources, and QA_ENV_SOURCE names only the url's
	// source — bash behavior kept for parity.
	rows := qaconfig.CollectRows()
	last := func(field string) (val string) {
		for _, r := range rows {
			if qaconfig.Field(r, 1) == envName && qaconfig.Field(r, 2) == field {
				val = qaconfig.Field(r, 3)
			}
		}
		return val
	}
	url, source := "", ""
	for _, r := range rows {
		if qaconfig.Field(r, 1) == envName && qaconfig.Field(r, 2) == "url" {
			url, source = qaconfig.Field(r, 3), qaconfig.Field(r, 4)
		}
	}

	if url == "" {
		fmt.Fprintf(os.Stderr, "bbs-qa-config probe: env '%s' not found\n", envName)
		return errSilent
	}

	fmt.Printf("QA_ENV_NAME=%s\n", qaconfig.ShQuote(envName))
	fmt.Printf("QA_ENV_URL=%s\n", qaconfig.ShQuote(url))
	fmt.Printf("QA_ENV_RUNTIME=%s\n", qaconfig.ShQuote(last("runtime")))
	fmt.Printf("QA_ENV_GUIDELINE=%s\n", qaconfig.ShQuote(last("guideline")))
	fmt.Printf("QA_ENV_USERNAME_ENV=%s\n", qaconfig.ShQuote(last("username_env")))
	fmt.Printf("QA_ENV_PASSWORD_ENV=%s\n", qaconfig.ShQuote(last("password_env")))
	fmt.Printf("QA_ENV_PREPARE=%s\n", qaconfig.ShQuote(qaconfig.CollectTopScalar("prepare")))
	fmt.Printf("QA_ENV_REVERT=%s\n", qaconfig.ShQuote(qaconfig.CollectTopScalar("revert")))
	fmt.Printf("QA_ENV_SOURCE=%s\n", qaconfig.ShQuote(source))
	return nil
}

func runQAConfigList() error {
	var names []string
	for _, r := range qaconfig.CollectRows() {
		names = append(names, qaconfig.Field(r, 1))
	}
	for _, n := range qaconfig.SortU(names) {
		fmt.Println(n)
	}
	return nil
}

func runQAConfigDefaultEnv() error {
	val := qaconfig.CollectDefaultEnv()
	if val == "" {
		// `[ -n "$val" ] && printf` as the function's last command returns 1,
		// which set -e turns into exit 1 — a bash bug (the doc says exit 0)
		// kept for parity.
		return errSilent
	}
	fmt.Println(val)
	return nil
}

func runQAConfigCheck() error {
	repo := qaconfig.RepoToplevel()
	errs := 0
	for _, f := range []string{repo + "/.babysit/qa.yaml", repo + "/.babysit/qa.local.yaml"} {
		if !qaconfig.FileExists(f) {
			continue
		}
		both := append(qaconfig.EnvironmentRows(f), qaconfig.SimpleLocalRows(f)...)
		var envsSeen, seenURLs []string
		for _, r := range both {
			envsSeen = append(envsSeen, qaconfig.Field(r, 1))
			if qaconfig.Field(r, 2) == "url" {
				seenURLs = append(seenURLs, qaconfig.Field(r, 1))
			}
		}
		urlSet := map[string]bool{}
		for _, e := range seenURLs {
			urlSet[e] = true
		}
		for _, e := range qaconfig.SortU(envsSeen) {
			if e == "" {
				continue
			}
			if !urlSet[e] {
				fmt.Fprintf(os.Stderr, "bbs-qa-config check: %s: env '%s' missing url\n", f, e)
				errs++
			}
		}
		if qaConfigLeakCheck(f) != 0 {
			errs++
		}
	}
	if errs > 0 {
		return errSilent
	}
	return nil
}

// qaConfigLeakCheck ports cmd_leak_check, returning its status (0/1/2).
func qaConfigLeakCheck(file string) int {
	if file == "" || !qaconfig.FileExists(file) {
		fmt.Fprintln(os.Stderr, "bbs-qa-config leak-check: file required")
		return 2
	}
	// qa.local.yaml is gitignored — operators may put real secrets there.
	if strings.HasSuffix(file, "qa.local.yaml") {
		return 0
	}
	hits := qaconfig.LeakCheckHits(file)
	if len(hits) > 0 {
		fmt.Fprintf(os.Stderr, "bbs-qa-config leak-check: %s contains inline credential literals:\n", file)
		for _, h := range hits {
			fmt.Fprintln(os.Stderr, h)
		}
		fmt.Fprintln(os.Stderr, "Use credentials.{username_env,password_env} (env-var names) or move secrets to .babysit/qa.local.yaml (gitignored).")
		return 1
	}
	return 0
}
