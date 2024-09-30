package sysbackup

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/log"
)

// ----------------------------------------------------------------------------
// PID handling
// ----------------------------------------------------------------------------
func managePIDFile(f string) (bool, error) {
	if pidFileExists(f) {
		pid, err := readPIDFile(f)
		if err != nil {
			return false, fmt.Errorf("failed to read PID file: %v", err)
		}

		if isPIDRunning(pid) {
			return true, nil
		}

		log.Warn("Stale PID file found, removing it", "pid", pid)
		if err := os.Remove(f); err != nil {
			return false, fmt.Errorf("failed to remove stale PID file: %v", err)
		}
	}

	pid := os.Getpid()
	if err := os.WriteFile(f, []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		return false, fmt.Errorf("failed to create PID file: %v", err)
	}

	return false, nil
}

func pidFileExists(f string) bool {
	_, err := os.Stat(f)
	return err == nil
}

func readPIDFile(f string) (int, error) {
	data, err := os.ReadFile(f)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}

	return pid, nil
}

func isPIDRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}
