package sysbackup

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
)

// ----------------------------------------------------------------------------
// Disk space and backup size estimation
// ----------------------------------------------------------------------------
// Check free space on the destination and fail if insufficient space
func (a App) validateFreeSpace() error {
	log.Info("Checking free space on target", "dir", a.cfg.Target)
	// Step 1: Get available disk space on the destination using `df`
	freeSpace, err := getFreeSpace(a.cfg.Target)
	if err != nil {
		return fmt.Errorf("failed to get free space: %v", err)
	}

	log.Info("Calculated free space on destination", "available", freeSpace)

	if a.cfg.EstimateBackupSize {
		// Step 2: Estimate the size of the backup using rsync's `--dry-run` option
		backupSize, err := estimateBackupSize(a.cfg.Source, a.cfg.Target)
		if err != nil {
			return fmt.Errorf("failed to estimate backup size: %v", err)
		}

		log.Info("Estimated backup size", "size", backupSize)

		// Step 3: Compare free space and backup size
		if backupSize > freeSpace {
			return fmt.Errorf("insufficient space on destination: need %d KB, but only %d KB is available", backupSize, freeSpace)
		}
	} else if a.cfg.MinFreeSpace > 0 {
		// cfg.MinFreeSpace is in MB, convert to KB
		minFreeSpace := int64(a.cfg.MinFreeSpace) * 1024
		if freeSpace < minFreeSpace {
			return fmt.Errorf("insufficient space on destination: need at least %d KB, but only %d KB is available", minFreeSpace, freeSpace)
		}
	}

	return nil
}

// Get the available free space on the destination using `df`
func getFreeSpace(destination string) (int64, error) {
	cmd := exec.Command("df", "--output=avail", destination)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to run df: %v", err)
	}

	output := strings.TrimSpace(out.String())
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output: %s", output)
	}

	freeSpace, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse free space: %v", err)
	}

	return freeSpace, nil
}

// Estimate the size of the backup using rsync's `--dry-run` option
func estimateBackupSize(source string, destination string) (int64, error) {
	cmd := exec.Command("rsync", "--dry-run", "--stats", "-a", source, destination)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to run rsync: %v", err)
	}

	// Parse the rsync stats output to get the "Total file size"
	output := out.String()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Total file size:") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				backupSize, err := strconv.ParseInt(parts[3], 10, 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse backup size: %v", err)
				}
				return backupSize / 1024, nil // Return size in KB
			}
		}
	}

	return 0, fmt.Errorf("could not find backup size in rsync output")
}
