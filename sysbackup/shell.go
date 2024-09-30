package sysbackup

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
)

// ----------------------------------------------------------------------------
// Shell command helpers
// ----------------------------------------------------------------------------
// runShellCommand runs a shell command and returns any errors.
func runShellCommand(cmd string) error {
	log.Info("Executing command", "cmd", cmd)
	command := exec.Command("sh", "-c", cmd)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("command failed: %v, %s", err, stderr.String())
	}
	return nil
}

// runCmdGetOutputString runs a shell command and returns the output as a string.
func runCmdGetOutputString(cmd string) (string, error) {
	log.Info("Executing command", "cmd", cmd)
	command := exec.Command("sh", "-c", cmd)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return "", fmt.Errorf("command failed: %v, %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// runCmdGetOutputLines runs a shell command and returns the output as a slice of strings.
func runCmdGetOutputLines(cmd string) ([]string, error) {
	output, err := runCmdGetOutputString(cmd)
	if err != nil {
		return nil, err
	}

	if output == "" {
		return nil, nil
	}

	return strings.Split(output, "\n"), nil
}
