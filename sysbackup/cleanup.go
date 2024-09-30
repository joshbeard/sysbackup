package sysbackup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// ----------------------------------------------------------------------------
// Cleanup Helpers
// ----------------------------------------------------------------------------
// Clean up old backups based on maximum number of backups to keep
// TODO: Implement maxAge cleanup. maxAge is number of days. This should be
// based on the timestamp in the backup directory name and reconcile with
// maxBackups. We should always have at least 1 backup. Neither, either, or
// both can be set.
func (a App) cleanupOldBackups() {
	if a.cfg.MaxBackups <= 0 && a.cfg.MaxAge <= 0 {
		log.Debug("maxBackups and maxAge aren't set, skipping cleanup")
		return
	}

	now := time.Now().Truncate(24 * time.Hour)
	backups, err := getBackups(a.cfg.Target)
	if err != nil {
		log.Error("Error reading backup directory for cleanup", "dir", a.cfg.Target, "err", err)
		return
	}

	sort.Strings(backups)

	if a.cfg.MaxAge > 0 {
		backups = a.cleanupByMaxAge(backups, now)
	}
	if a.cfg.MaxBackups > 0 && len(backups) > a.cfg.MaxBackups {
		cleanupByMaxBackups(backups, a.cfg.Target, a.cfg.MaxBackups)
	}
}

// Retrieve backups, either remote or local
func getBackups(backupDir string) ([]string, error) {
	if isRemotePath(backupDir) {
		remoteHost, remotePath := parseRemotePath(backupDir)
		sshCmd := fmt.Sprintf(`ssh %s "ls -1 %s"`, remoteHost, remotePath)
		output, err := runCmdGetOutputString(sshCmd)
		if err != nil {
			return nil, err
		}
		return filterBackups(strings.Split(strings.TrimSpace(output), "\n")), nil
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasSuffix(entry.Name(), ".inprogress") {
			backups = append(backups, entry.Name())
		}
	}
	return backups, nil
}

// Filter out ".inprogress" entries
func filterBackups(entries []string) []string {
	var backups []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry, ".inprogress") {
			backups = append(backups, entry)
		}
	}
	return backups
}

// Clean up backups that exceed the maxAge and return remaining ones
func (a App) cleanupByMaxAge(backups []string, now time.Time) []string {
	var remainingBackups []string
	for i := 0; i < len(backups)-1; i++ { // Keep at least one backup
		if isBackupTooOld(backups[i], a.cfg.DateFormat, a.cfg.MaxAge, now) {
			removeBackup(a.cfg.Target, backups[i])
		} else {
			// removeBackup(backupDir, backups[i])
			remainingBackups = append(remainingBackups, backups[i])
		}
	}
	remainingBackups = append(remainingBackups, backups[len(backups)-1]) // Always keep the most recent
	return remainingBackups
}

// Clean up backups that exceed the maxBackups limit
func cleanupByMaxBackups(backups []string, backupDir string, maxBackups int) {
	toRemove := len(backups) - maxBackups
	for i := 0; i < toRemove; i++ {
		removeBackup(backupDir, backups[i])
	}
}

// Remove backup directory (works for both remote and local)
func removeBackup(baseDir, backup string) {
	removeDir := filepath.Join(baseDir, backup)
	log.Info("Removing old backup", "dir", removeDir)

	if isRemotePath(baseDir) {
		remoteHost, remotePath := parseRemotePath(baseDir)
		sshCmd := fmt.Sprintf(`ssh %s "rm -rf %s/%s"`, remoteHost, remotePath, backup)
		if err := runShellCommand(sshCmd); err != nil {
			log.Error("Failed to remove remote backup", "dir", removeDir, "err", err)
		}
	} else {
		if err := os.RemoveAll(removeDir); err != nil {
			log.Error("Failed to remove local backup", "dir", removeDir, "err", err)
		}
	}
}

func isBackupTooOld(backupName, dateFormat string, maxAge int, now time.Time) bool {
	dateFormats := []string{
		dateFormat,
		"2006-01-02",
		"2006-01-02-15-04",
	}

	// Normalize backup time for day-level comparison
	now = now.Truncate(24 * time.Hour)

	for _, format := range dateFormats {
		if parsedTime, err := time.Parse(format, backupName); err == nil {
			// Normalize backup time for day-level comparison
			parsedTime = parsedTime.Truncate(24 * time.Hour)
			daysOld := int(now.Sub(parsedTime).Hours() / 24)

			if daysOld > maxAge {
				return true
			}
			return false
		}
	}

	return false
}

// cleanupStaleInprogressDirs removes stale in-progress directories.
// It supports both local and remote targets.
func (a App) cleanupStaleInprogressDirs() {
	if isRemotePath(a.cfg.Target) {
		// Remote cleanup
		remoteHost, remotePath := parseRemotePath(a.cfg.Target)

		// Find all remote in-progress directories
		sshCmd := fmt.Sprintf(`ssh %s "find %s -type d -maxdepth 1 -name '*.inprogress.*'"`, remoteHost, remotePath)
		output, err := runCmdGetOutputString(sshCmd)
		if err != nil {
			log.Error("Error finding in-progress directories on remote host", "err", err)
			return
		}

		// Iterate over remote directories
		directories := strings.Split(strings.TrimSpace(output), "\n")
		for _, dir := range directories {
			if a.cfg.DeleteStaleInprogressDirs {
				removeRemoteDir(remoteHost, dir)
			} else {
				log.Warn("Stale in-progress directory found", "dir", dir)
			}
		}
	} else {
		// Local cleanup
		entries, err := os.ReadDir(a.cfg.Target)
		if err != nil {
			log.Error("Error reading backup directory for cleanup", "dir", a.cfg.Target, "err", err)
			return
		}

		// Iterate over local directories
		for _, entry := range entries {
			if entry.IsDir() && strings.Contains(entry.Name(), ".inprogress.") {
				dirPath := filepath.Join(a.cfg.Target, entry.Name())
				if a.cfg.DeleteStaleInprogressDirs {
					log.Info("Removing stale in-progress directory", "dir", dirPath)
					if err := os.RemoveAll(dirPath); err != nil {
						log.Error("Failed to remove stale in-progress directory", "dir", dirPath, "err", err)
						continue
					}
				} else {
					log.Warn("Stale in-progress directory found", "dir", dirPath)
				}
			}
		}
	}
}

// removeRemoteDir removes a directory on a remote host.
func removeRemoteDir(host, dir string) error {
	removeCmd := fmt.Sprintf(`ssh %s "rm -rf %s"`, host, dir)
	if err := runShellCommand(removeCmd); err != nil {
		return err
	}

	log.Info("Removed directory on remote host", "host", host, "dir", dir)
	return nil
}
