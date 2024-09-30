package sysbackup

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
)

type rsyncCommand struct {
	source     string
	target     string
	linkDest   string
	filterFile string
	rsyncArgs  string
}

// ----------------------------------------------------------------------------
// Rsync command helpers
// ----------------------------------------------------------------------------
// buildCmd constructs the rsync command based on the rsyncCommand's fields.
func (r *rsyncCommand) build() *exec.Cmd {
	args := []string{
		"--archive",
		"--verbose",
		"--delete",
		"--stats",
		"--info=progress2",
		"--progress",
	}

	// Add --link-dest for incremental backups if a previous backup exists
	if r.linkDest != "" {
		args = append(args, fmt.Sprintf("--link-dest=%s", r.linkDest))
	}

	// Add the filter file if specified
	if r.filterFile != "" {
		args = append(args, fmt.Sprintf("--filter=merge %s", r.filterFile))
	}

	// Add custom rsync arguments from flags or config
	if r.rsyncArgs != "" {
		args = append(args, strings.Split(r.rsyncArgs, " ")...)
	}

	// Add the source and destination directories
	args = append(args, r.source, r.target)

	// Return the exec.Cmd for rsync
	return exec.Command("rsync", args...)
}

// func (r *rsyncCommand) run() error {
func (s *App) run() error {
	cmd := s.rsyncCmd.build()

	// Capture stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	log.Info("Running rsync", "args", cmd.Args)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rsync: %v", err)
	}

	// Conditionally log output
	if s.cfg.LogRsync {
		err = s.rsyncCmd.logOutput(stdoutPipe, stderrPipe)
	} else {
		// If not logging, we still need to consume the output to prevent blocking
		go io.Copy(io.Discard, stdoutPipe)
		go io.Copy(io.Discard, stderrPipe)
	}

	if err != nil {
		return err
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		log.Error("Rsync failed", "err", err)
		return err
	}

	log.Info("Rsync completed successfully")
	return nil
}

// logOutput handles the logging of stdout and stderr for rsync command
func (r *rsyncCommand) logOutput(stdout, stderr io.ReadCloser) error {
	done := make(chan error, 1)

	// Function to handle scanning and logging of command output
	handleOutput := func(outputType string, scanner *bufio.Scanner) {
		for scanner.Scan() {
			line := scanner.Text()
			log.Debug("[rsync]", "output", outputType, "msg", line)
		}
		if err := scanner.Err(); err != nil {
			done <- fmt.Errorf("error reading %s: %v", outputType, err)
		}
	}

	go func() {
		stdoutScanner := bufio.NewScanner(stdout)
		handleOutput("stdout", stdoutScanner)

		stderrScanner := bufio.NewScanner(stderr)
		handleOutput("stderr", stderrScanner)

		close(done)
	}()

	return <-done
}

// run executes the rsync command and captures stdout and stderr.
// FIXME: if cfg.Log.LogRsync is true, log the output. Otherwise, don't.
func (r *rsyncCommand) xrun() error {
	cmd := r.build()

	// Capture stdout and stderr using MultiWriter
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	log.Info("Running rsync", "args", cmd.Args)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rsync: %v", err)
	}

	// Create channels to stream stdout and stderr in real-time
	done := make(chan error)

	// Stream stdout
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			log.Debug("[rsync]", "output", "stdout", "msg", line)
		}
		if err := scanner.Err(); err != nil {
			done <- fmt.Errorf("error reading stdout: %v", err)
		}
	}()

	// Stream stderr
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			log.Debug("[rsync]", "output", "stderr", "msg", line)
		}
		if err := scanner.Err(); err != nil {
			done <- fmt.Errorf("error reading stderr: %v", err)
		}
	}()

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		log.Error("Rsync failed", "err", err)
		return err
	}

	// Close the done channel
	close(done)

	log.Info("Rsync completed successfully")
	return nil
}
