//go:build unix

package cmd

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func processStartIdentity(pid int) (string, bool) {
	if pid <= 0 || !processAlive(pid) {
		return "", false
	}
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	start := strings.Join(strings.Fields(string(out)), " ")
	return start, start != ""
}
