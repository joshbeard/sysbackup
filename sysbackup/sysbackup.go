package sysbackup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

type App struct {
	cfg       Config
	backupDir string
	rsyncCmd  *rsyncCommand
}

// ----------------------------------------------------------------------------
// Backup command
// ----------------------------------------------------------------------------
// func (s App) Run(cmd *cobra.Command, args []string, cfg Config) {
func Run(cfg Config) {
	app := App{
		cfg: cfg,
	}

	// Check for another instance running
	running, err := managePIDFile(cfg.PidFile)
	if err != nil {
		log.Fatal("Error managing PID file", "err", err)
	}

	if running {
		log.Fatal("Another instance is already running")
	}

	// Check if the remote host is reachable (for remote sources/targets)
	if isRemotePath(cfg.Source) && !pingHost(cfg.Source) {
		log.Fatal("Remote source host is unreachable", "host", cfg.Source)
	}
	if isRemotePath(cfg.Target) && !pingHost(cfg.Target) {
		log.Fatal("Remote target host is unreachable", "host", cfg.Target)
	}

	if !targetExists(cfg.Target) {
		if !cfg.CreateTarget {
			log.Fatal("Use --create-target to create the target directory")
		}

		if err := createTargetDir(cfg.Target); err != nil {
			log.Fatal("Failed to create target directory", "dir", cfg.Target, "err", err)
		}
	}

	app.cleanupOldBackups()
	app.cleanupStaleInprogressDirs()

	log.Info("Starting backup...")

	timestamp := time.Now().Format(cfg.DateFormat)
	inProgressDir := ""
	targetInprogress := ""
	finalDir := filepath.Join(cfg.Target, timestamp)

	// If the finalDir exists, error
	if _, err := os.Stat(finalDir); err == nil {
		log.Fatal("Backup directory already exists", "dir", finalDir)
	}

	if !isRemotePath(cfg.Target) {
		inProgressDir = fmt.Sprintf("%s/%s.inprogress", cfg.Target, timestamp)
		targetInprogress = inProgressDir

	} else {
		// If the target is remote, create the remote in-progress directory over SSH
		remoteHost, remotePath := parseRemotePath(cfg.Target)
		baseDir := fmt.Sprintf("%s.inprogress", timestamp)
		inProgressDir = fmt.Sprintf("%s/%s", remotePath, baseDir)
		targetInprogress = fmt.Sprintf("%s/%s", cfg.Target, baseDir)

		log.Info("Creating remote in-progress directory", "dir", inProgressDir)
		sshCmd := fmt.Sprintf(`ssh %s "mkdir -p %s"`, remoteHost, inProgressDir)
		if err := runShellCommand(sshCmd); err != nil {
			log.Fatal("Failed to create remote in-progress directory", "dir", inProgressDir)
		}
	}

	// Check free space and calculate backup size
	if cfg.CheckFreeSpace {
		// if err := validateFreeSpace(target, source); err != nil {
		if err := app.validateFreeSpace(); err != nil {
			log.Fatal("Error checking free space", "err", err)
		}
	}

	linkDest := app.findLatestBackup()
	if isRemotePath(cfg.Target) && linkDest != "" {
		remoteHost, path := parseRemotePath(linkDest)
		linkDest = path

		if !strings.HasPrefix(path, "/") {
			sshCmd := fmt.Sprintf(`ssh %s "realpath %s"`, remoteHost, path)
			linkDest, err = runCmdGetOutputString(sshCmd)
			if err != nil {
				log.Fatal("Failed to get absolute path of link destination", "err", err)
			}
		}
	} else {
		linkDest, _ = filepath.Abs(linkDest)
	}

	// Build and run the rsync command
	log.Info("backing up", "dir", targetInprogress)
	app.rsyncCmd = &rsyncCommand{
		source:     cfg.Source,
		target:     targetInprogress,
		linkDest:   linkDest,
		filterFile: cfg.FilterFile,
		rsyncArgs:  cfg.RsyncArgs,
	}
	if err := app.run(); err != nil {
		log.Fatal("Rsync failed", "err", err)
	}

	// Rename in-progress directory to final name
	if err := renameInProgressDir(inProgressDir, finalDir); err != nil {
		log.Fatal("Failed to rename in-progress directory", "err", err)
	}

	// Create a "Latest" symlink pointing to the new backup
	if err := createLatestSymlink(finalDir); err != nil {
		log.Fatal("Failed to create Latest symlink", "err", err)
	}

	log.Info("Backup completed successfully.")
}

func targetExists(target string) bool {
	if isRemotePath(target) {
		remoteHost, remotePath := parseRemotePath(target)
		sshCmd := fmt.Sprintf(`ssh %s "test -d %s"`, remoteHost, remotePath)
		if err := runShellCommand(sshCmd); err != nil {
			return false
		}

		return true
	}

	if _, err := os.Stat(target); os.IsNotExist(err) {
		return false
	}

	return true
}

func createTargetDir(target string) error {
	// Handle remote target
	if isRemotePath(target) {
		remoteHost, remotePath := parseRemotePath(target)
		sshCmd := fmt.Sprintf(`ssh %s "mkdir -p %s"`, remoteHost, remotePath)
		if err := runShellCommand(sshCmd); err != nil {
			return fmt.Errorf("failed to create remote target directory: %v", err)
		}
	} else {
		// Handle local target
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("failed to create target directory: %v", err)
		}
	}

	return nil
}

func createLatestSymlink(finalDir string) error {
	if isRemotePath(finalDir) {
		remoteHost, remoteDir := parseRemotePath(finalDir)
		parentDir := filepath.Dir(remoteDir)

		sshCmd := fmt.Sprintf(`ssh %s "ln -sfrn %s %s/Latest"`, remoteHost, remoteDir, parentDir)
		if err := runShellCommand(sshCmd); err != nil {
			return fmt.Errorf("failed to create remote symlink: %v", err)
		}
	} else {
		parentDir := filepath.Dir(finalDir)
		latestLink := filepath.Join(parentDir, "Latest")

		if err := os.Remove(latestLink); err != nil && !os.IsNotExist(err) {
			fmt.Errorf("failed to remove existing symlink: %v", err)
		}

		baseDir := filepath.Base(finalDir)
		if err := os.Symlink(baseDir, latestLink); err != nil {
			return fmt.Errorf("failed to create symlink: %v", err)
		}
	}

	return nil
}

func renameInProgressDir(inProgressDir, target string) error {
	if isRemotePath(target) {
		remoteHost, remotePath := parseRemotePath(target)
		sshCmd := fmt.Sprintf(`ssh %s "mv %s %s"`, remoteHost, inProgressDir, remotePath)
		if err := runShellCommand(sshCmd); err != nil {
			return fmt.Errorf("failed to rename remote in-progress directory: %v", err)
		}
	} else {
		if err := os.Rename(inProgressDir, target); err != nil {
			return fmt.Errorf("failed to rename in-progress directory: %v", err)
		}
	}

	return nil
}

// Find the most recent backup directory (to use as --link-dest)
// Supports both local and remote targets.
func (a App) findLatestBackup() string {
	var entries []string
	var err error

	if isRemotePath(a.cfg.Target) {
		// Remote case: use SSH to list directories on the remote host
		remoteHost, remotePath := parseRemotePath(a.cfg.Target)
		sshCmd := fmt.Sprintf(`ssh %s "ls -1 %s"`, remoteHost, remotePath)
		entries, err = runCmdGetOutputLines(sshCmd)
		if err != nil {
			log.Error("Error reading remote backup directory", "err", err)
			return ""
		}
	} else {
		// Local case: use os.ReadDir to list directories
		localEntries, err := os.ReadDir(a.cfg.Target)
		if err != nil {
			log.Error("Error reading backup directory", "err", err)
			return ""
		}
		for _, entry := range localEntries {
			entries = append(entries, entry.Name())
		}
	}

	var latestBackup string
	var latestTime time.Time

	// Process the directory entries (both local and remote cases)
	for _, entry := range entries {
		if strings.HasSuffix(entry, ".inprogress") {
			continue
		}

		timestamp, err := time.Parse(a.cfg.DateFormat, entry)
		if err == nil && timestamp.After(latestTime) {
			latestBackup = filepath.Join(a.cfg.Target, entry)
			latestTime = timestamp
		}
	}

	if latestBackup != "" {
		log.Info("Found latest backup", "dir", latestBackup)
	} else {
		log.Info("No previous backup found. This will be a full backup.")
	}

	return latestBackup
}
