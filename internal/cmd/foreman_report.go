package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/babysit/internal/identity"
)

// The coordinator reconciles live sources; this reader remains usable after
// Orca and the coordinator close. Never present that snapshot as a live check.
func foremanReport(args []string) error {
	if len(args) != 1 || args[0] == "" || args[0] == "." || args[0] == ".." || strings.ContainsAny(args[0], `/\`) || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: bbs foreman report <parent-ticket>")
	}
	return readForemanReport(identity.Resolve().ProjectHome, args[0])
}

func readForemanReport(projectHome, parent string) error {
	path := filepath.Join(projectHome, "tickets", parent, "report.md")
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("foreman report: %w; ask the owning foreman to reconcile this project", err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("foreman report: %s is empty; project completion is unknown", path)
	}
	fmt.Printf("Saved reconciliation snapshot (not a live check): %s\n\n%s", path, body)
	return nil
}
