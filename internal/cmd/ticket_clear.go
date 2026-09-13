package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reallongnguyen/babysit/internal/identity"
	"github.com/reallongnguyen/babysit/internal/ticket"
)

// runClear intentionally has one explicit destructive shape. The dashboard's
// delete operation moves a single record to trash; this is its CLI counterpart
// for clearing the active ticket records across every project.
func runClear(args []string) {
	if len(args) != 1 || args[0] != "--all" {
		fmt.Fprintln(os.Stderr, "usage: bbs ticket clear --all")
		os.Exit(2)
	}

	cleared, err := clearAllTickets(identity.BabysitHome())
	if err != nil {
		fmt.Fprintln(os.Stderr, "clear:", err)
		os.Exit(1)
	}
	fmt.Printf("CLEARED=%d\n", cleared)
}

func clearAllTickets(stateDir string) (int, error) {
	projects, err := os.ReadDir(filepath.Join(stateDir, "projects"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	type target struct{ project, ticket string }
	var targets []target
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		ids, err := ticket.TicketIDs(filepath.Join(stateDir, "projects", project.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		for _, id := range ids {
			targets = append(targets, target{project.Name(), id})
		}
	}

	for i, target := range targets {
		st := ticket.New(identity.Env{
			Slug:        target.project,
			Ticket:      target.ticket,
			ProjectHome: filepath.Join(stateDir, "projects", target.project),
		})
		if _, err := trashTicket(stateDir, target.project, st); err != nil {
			return i, err
		}
	}
	return len(targets), nil
}
