package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var (
	version     = "dev"
	commit      = "none"
	date        = "unknown"
	fullVersion = fmt.Sprintf("%s, commit %s, built at %s", version, commit, date)
)

var about = `
A simple rsync-based incremental backup tool
https://github.com/joshbeard/sysbackup

If a configuration file is found at the following locations, it will be used:
  - $XDG_CONFIG_HOME/sysbackup/sysbackup.yaml
  - $XDG_CONFIG_HOME/sysbackup/sysbackup.yml
  - $HOME/.sysbackup/sysbackup.yaml
  - $HOME/.sysbackup/sysbackup.yml
  - $HOME/.sysbackup.yaml
  - $HOME/.sysbackup.yml
  - /etc/sysbackup/sysbackup.yaml
  - /etc/sysbackup/sysbackup.yml

Logging:
  - By default, logs are written to stderr
  - To log rsync output, set the '--log-file=/path/to/logfile' argument
    using the 'rsync_args' configuration option or the '--rsync-args' flag
  - rsync can also be logged to stderr alongside sysbackup logs using the
    '--log-rsync' flag

All flags can be set in the configuration file or as environment variables.
`

var examples = `
  # Backup /path/to/source to /path/to/backup
  sysbackup /path/to/source /path/to/backup

  # Generate a default configuration file
  sysbackup --gen-config
`

var (
	configFile string
	cfg        config
)

var ctx = context.Background()

type config struct {
	Source                    string `mapstructure:"source" yaml:"source"`
	Target                    string `mapstructure:"target" yaml:"target"`
	FilterFile                string `mapstructure:"filter_file" yaml:"filter_file"`
	PidFile                   string `mapstructure:"pid_file" yaml:"pid_file"`
	RsyncArgs                 string `mapstructure:"rsync_args" yaml:"rsync_args"`
	DateFormat                string `mapstructure:"date_format" yaml:"date_format"`
	MaxBackups                int    `mapstructure:"max_backups" yaml:"max_backups"`
	DeleteStaleInprogressDirs bool   `mapstructure:"delete_stale_inprogress_dirs" yaml:"delete_stale_inprogress_dirs"`
	CheckFreeSpace            bool   `mapstructure:"check_free_space" yaml:"check_free_space"`
	EstimateBackupSize        bool   `mapstructure:"estimate_backup_size" yaml:"estimate_backup_size"`
	MinFreeSpace              int    `mapstructure:"min_free_space" yaml:"min_free_space"`
	CreateTarget              bool   `mapstructure:"create_target" yaml:"create_target"`
	LogLevel                  string `mapstructure:"log_level" yaml:"log_level"`
	LogRsync                  bool   `mapstructure:"log_rsync" yaml:"log_rsync"`
	genConfig                 bool
}

type rsyncCommand struct {
	source     string
	target     string
	linkDest   string
	filterFile string
	rsyncArgs  string
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "sysbackup [flags] [source] [target]",
		Short:   "Rsync-based incremental backup tool",
		Example: strings.TrimPrefix(examples, "\n"),
		Long:    fmt.Sprintf("sysbackup v%s\n", fullVersion) + strings.TrimPrefix(about, "\n"),
		Version: fullVersion,
		Run: func(cmd *cobra.Command, args []string) {
			if cfg.genConfig {
				out, err := genConfig()
				if err != nil {
					log.Error("Failed to generate config", "err", err)
					os.Exit(1)
				}

				fmt.Println(out)
				os.Exit(0)
			}

			setupConfig()
			setupLogging()
			runBackup(cmd, args)
		},
	}

	setupFlags(rootCmd)
	setupLogging()

	handleInterrupt()

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	quit()
}

// ----------------------------------------------------------------------------
// Setup and initialization
// ----------------------------------------------------------------------------
func setupFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&configFile,
		"config", "c", "",
		"config file (default is $HOME/.backup.yaml)")

	cmd.PersistentFlags().StringVarP(&cfg.RsyncArgs,
		"rsync-args", "r", "",
		"Custom rsync arguments")

	cmd.PersistentFlags().StringVarP(&cfg.FilterFile,
		"filter-file", "f", "",
		"Path to rsync filter file")

	defaultPidFile, err := xdg.RuntimeFile("sysbackup.pid")
	if err != nil {
		log.Fatal("Error getting default PID file", "err", err)
	}
	cmd.PersistentFlags().StringVar(&cfg.PidFile,
		"pid-file", defaultPidFile,
		"Path to the PID file")

	cmd.PersistentFlags().IntVarP(&cfg.MaxBackups,
		"max-backups", "m", 0,
		"Maximum number of backups to keep (0 for no limit)")

	cmd.PersistentFlags().BoolVar(&cfg.DeleteStaleInprogressDirs,
		"delete-stale-inprogress-dirs", false,
		"Delete stale in-progress directories")

	cmd.PersistentFlags().BoolVar(&cfg.CheckFreeSpace,
		"check-free-space", false,
		"Check free space on the destination before backup")

	cmd.PersistentFlags().BoolVar(&cfg.EstimateBackupSize,
		"estimate-backup-size", false,
		"Estimate backup size before running rsync")

	cmd.PersistentFlags().IntVar(&cfg.MinFreeSpace,
		"min-free-space", 0,
		"Minimum free space required on the destination (in MB)")

	cmd.PersistentFlags().StringVar(&cfg.DateFormat,
		"date-format", "2006-01-02-15-04",
		"Date format for backup directories")

	cmd.PersistentFlags().StringVar(&cfg.LogLevel,
		"log-level", "info",
		"Log level (debug, info, warn, error)")

	cmd.PersistentFlags().BoolVar(&cfg.LogRsync,
		"log-rsync", false,
		"Include rsync output in logs")

	cmd.PersistentFlags().StringVarP(&cfg.Source,
		"source", "s", "",
		"Backup source directory (or first positional argument)")

	cmd.PersistentFlags().StringVarP(&cfg.Target,
		"target", "t", "",
		"Backup target directory (or second positional argument)")

	cmd.PersistentFlags().BoolVar(&cfg.CreateTarget,
		"create-target", true,
		"Create the target directory if it doesn't exist")

	cmd.PersistentFlags().BoolVarP(&cfg.genConfig, "gen-config", "g", false,
		"Generate a default config file and exit")

	viper.BindPFlag("rsync_args", cmd.PersistentFlags().Lookup("rsync-args"))
	viper.BindPFlag("source", cmd.PersistentFlags().Lookup("source"))
	viper.BindPFlag("target", cmd.PersistentFlags().Lookup("target"))
	viper.BindPFlag("filter_file", cmd.PersistentFlags().Lookup("filter-file"))
	viper.BindPFlag("max_backups", cmd.PersistentFlags().Lookup("max-backups"))
	viper.BindPFlag("pid_file", cmd.PersistentFlags().Lookup("pid-file"))
	viper.BindPFlag("delete_stale_inprogress_dirs", cmd.PersistentFlags().Lookup("delete-stale-inprogress-dirs"))
	viper.BindPFlag("check_free_space", cmd.PersistentFlags().Lookup("check-free-space"))
	viper.BindPFlag("estimate_backup_size", cmd.PersistentFlags().Lookup("estimate-backup-size"))
	viper.BindPFlag("min_free_space", cmd.PersistentFlags().Lookup("min-free-space"))
	viper.BindPFlag("create_target", cmd.PersistentFlags().Lookup("create-target"))
	viper.BindPFlag("date_format", cmd.PersistentFlags().Lookup("date-format"))

	viper.BindPFlag("log_level", cmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("log_rsync", cmd.PersistentFlags().Lookup("log-rsync"))
}

func genConfig() (string, error) {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func configPaths() []string {
	paths := []string{
		filepath.Join(xdg.ConfigHome, "sysbackup", "sysbackup.yaml"),
		filepath.Join(xdg.ConfigHome, "sysbackup", "sysbackup.yml"),

		filepath.Join(xdg.Home, ".sysbackup", "sysbackup.yaml"),
		filepath.Join(xdg.Home, ".sysbackup", "sysbackup.yml"),

		filepath.Join(xdg.Home, ".sysbackup.yaml"),
		filepath.Join(xdg.Home, ".sysbackup.yml"),
	}

	for _, dir := range xdg.ConfigDirs {
		paths = append(paths, filepath.Join(dir, "sysbackup", "sysbackup.yaml"))
		paths = append(paths, filepath.Join(dir, "sysbackup", "sysbackup.yml"))
	}

	paths = append(paths, "/etc/sysbackup/sysbackup.yaml")
	paths = append(paths, "/etc/sysbackup/sysbackup.yml")

	return paths
}

func setupConfig() error {
	if configFile == "" {
		configFile = findConfigFile()
	}

	if configFile == "" {
		log.Info("No config file found, using default settings")
		return nil
	}

	viper.SetConfigFile(configFile)
	if err := viper.ReadInConfig(); err == nil {
		log.Info("Loaded config file", "config", viper.ConfigFileUsed())
	} else {
		log.Info("Error loading config file", "error", err)
	}

	if err := viper.UnmarshalExact(&cfg); err != nil {
		log.Error("Error unmarshaling config", "error", err)
	}

	return nil
}

func findConfigFile() string {
	for _, path := range configPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func setupLogging() error {
	logH := os.Stderr

	if cfg.LogLevel == "" {
		logH, err := os.Open(os.DevNull)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		logger := log.New(logH)
		log.SetDefault(logger)

		return nil
	}

	logger := log.New(logH)

	logLevel, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("could not parse log level: %w", err)
	}

	logger.SetLevel(logLevel)
	logger.SetPrefix("sysbackup")
	logger.SetOutput(logH)
	logger.SetReportTimestamp(true)

	if logLevel == log.DebugLevel {
		logger.SetReportCaller(true)
	}

	log.SetDefault(logger)

	return nil
}

// Handle Ctrl-C (cleanup)
func handleInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info("Received Ctrl-C, cleaning up...")
		quit()
		os.Exit(1)
	}()
}

// Quit actions, such as removing the PID file
func quit() {
	os.Remove(cfg.PidFile)
}

// ----------------------------------------------------------------------------
// Backup command
// ----------------------------------------------------------------------------
func runBackup(cmd *cobra.Command, args []string) {
	// Check for another instance running
	running, err := managePIDFile(cfg.PidFile)
	if err != nil {
		log.Fatal("Error managing PID file", "err", err)
	}

	if running {
		log.Fatal("Another instance is already running")
	}

	// source and target can be set in the CFG or as CLI arguments.
	// they can be set using the flags or as positional arguments.
	// positional arguments take precedence over flags, and flags take
	// precedence over the config file.
	source := cfg.Source
	target := cfg.Target

	if len(args) == 1 || len(args) > 2 {
		log.Info("FAILED: Invalid number of arguments: %d", len(args))
		log.Info("Usage: %s", cmd.Use)

		os.Exit(1)
	}

	if len(args) == 2 {
		source = args[0]
		target = args[1]
	}

	// Check if the remote host is reachable (for remote sources/targets)
	if isRemotePath(source) && !pingHost(source) {
		log.Fatal("Remote source host is unreachable", "host", source)
	}
	if isRemotePath(target) && !pingHost(target) {
		log.Fatal("Remote target host is unreachable", "host", target)
	}

	if !targetExists(target) {
		if !cfg.CreateTarget {
			log.Fatal("Use --create-target to create the target directory")
		}

		if err := createTargetDir(target); err != nil {
			log.Fatal("Failed to create target directory", "dir", target)
		}
	}

	cleanupOldBackups(target, cfg.MaxBackups)
	cleanupStaleInprogressDirs(target)

	log.Info("Starting backup...")

	timestamp := time.Now().Format(cfg.DateFormat)
	inProgressDir := ""
	targetInprogress := ""
	finalDir := filepath.Join(target, timestamp)

	// If the finalDir exists, error
	if _, err := os.Stat(finalDir); err == nil {
		log.Fatal("Backup directory already exists", "dir", finalDir)
	}

	if !isRemotePath(target) {
		inProgressDir = filepath.Join(target, timestamp+".inprogress")
		targetInprogress = inProgressDir

	} else {
		// If the target is remote, create the remote in-progress directory over SSH
		remoteHost, remotePath := parseRemotePath(target)
		baseDir := fmt.Sprintf("%s.inprogress", timestamp)
		inProgressDir = fmt.Sprintf("%s/%s", remotePath, baseDir)
		targetInprogress = fmt.Sprintf("%s/%s", target, baseDir)

		log.Info("Creating remote in-progress directory", "dir", inProgressDir)
		sshCmd := fmt.Sprintf(`ssh %s "mkdir -p %s"`, remoteHost, inProgressDir)
		if err := runShellCommand(sshCmd); err != nil {
			log.Fatal("Failed to create remote in-progress directory", "dir", inProgressDir)
		}
	}

	// Check free space and calculate backup size
	if cfg.CheckFreeSpace {
		if err := validateFreeSpace(target, source); err != nil {
			log.Fatal("Error checking free space", "err", err)
		}
	}

	linkDest := findLatestBackup(target)
	if isRemotePath(target) && linkDest != "" {
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
	rsyncCmd := &rsyncCommand{
		source:     source,
		target:     targetInprogress,
		linkDest:   linkDest,
		filterFile: cfg.FilterFile,
		rsyncArgs:  cfg.RsyncArgs,
	}
	if err := rsyncCmd.run(); err != nil {
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
	quit()
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
func findLatestBackup(backupDir string) string {
	var entries []string
	var err error

	if isRemotePath(backupDir) {
		// Remote case: use SSH to list directories on the remote host
		remoteHost, remotePath := parseRemotePath(backupDir)
		sshCmd := fmt.Sprintf(`ssh %s "ls -1 %s"`, remoteHost, remotePath)
		entries, err = runCmdGetOutputLines(sshCmd)
		if err != nil {
			log.Error("Error reading remote backup directory", "err", err)
			return ""
		}
	} else {
		// Local case: use os.ReadDir to list directories
		localEntries, err := os.ReadDir(backupDir)
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

		timestamp, err := time.Parse(cfg.DateFormat, entry)
		if err == nil && timestamp.After(latestTime) {
			latestBackup = filepath.Join(backupDir, entry)
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

// ----------------------------------------------------------------------------
// Remote Helpers
// ----------------------------------------------------------------------------
// isRemotePath checks if the path is a remote one by looking for a colon (:)
func isRemotePath(path string) bool {
	return strings.Contains(path, ":")
}

// parseRemotePath splits a remote path into user@host and the remote directory
func parseRemotePath(remotePath string) (string, string) {
	parts := strings.SplitN(remotePath, ":", 2)
	if len(parts) != 2 {
		log.Fatal("Invalid remote path format", "path", remotePath)
	}
	return parts[0], parts[1]
}

// Ping the remote host to check if it's reachable
func pingHost(source string) bool {
	host := strings.Split(source, ":")[0]
	cmd := exec.Command("ping", "-c", "1", host)
	err := cmd.Run()
	return err == nil
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

func (r *rsyncCommand) run() error {
	cmd := r.build()

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
	if cfg.LogRsync {
		err = r.logOutput(stdoutPipe, stderrPipe)
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

// ----------------------------------------------------------------------------
// Disk space and backup size estimation
// ----------------------------------------------------------------------------
// Check free space on the destination and fail if insufficient space
func validateFreeSpace(destination string, source string) error {
	log.Info("Checking free space on destination", "dir", destination)
	// Step 1: Get available disk space on the destination using `df`
	freeSpace, err := getFreeSpace(destination)
	if err != nil {
		return fmt.Errorf("failed to get free space: %v", err)
	}

	log.Info("Calculated free space on destination", "available", freeSpace)

	if cfg.EstimateBackupSize {
		// Step 2: Estimate the size of the backup using rsync's `--dry-run` option
		backupSize, err := estimateBackupSize(source, destination)
		if err != nil {
			return fmt.Errorf("failed to estimate backup size: %v", err)
		}

		log.Info("Estimated backup size", "size", backupSize)

		// Step 3: Compare free space and backup size
		if backupSize > freeSpace {
			return fmt.Errorf("insufficient space on destination: need %d KB, but only %d KB is available", backupSize, freeSpace)
		}
	} else if cfg.MinFreeSpace > 0 {
		// cfg.MinFreeSpace is in MB, convert to KB
		minFreeSpace := int64(cfg.MinFreeSpace) * 1024
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

// ----------------------------------------------------------------------------
// Cleanup Helpers
// ----------------------------------------------------------------------------
// Clean up old backups based on maximum number of backups to keep
func cleanupOldBackups(backupDir string, maxBackups int) {
	if maxBackups <= 0 {
		return
	}

	if isRemotePath(backupDir) {
		// Remote cleanup
		remoteHost, remotePath := parseRemotePath(backupDir)
		sshCmd := fmt.Sprintf(`ssh %s "ls -1 %s"`, remoteHost, remotePath)
		output, err := runCmdGetOutputString(sshCmd)
		if err != nil {
			log.Error("Error reading remote backup directory for cleanup", "err", err)
			return
		}

		entries := strings.Split(strings.TrimSpace(output), "\n")
		var backups []string

		for _, entry := range entries {
			if strings.HasSuffix(entry, ".inprogress") {
				continue
			}

			backups = append(backups, entry)
		}

		if len(backups) > maxBackups {
			toRemove := len(backups) - maxBackups
			for i := 0; i < toRemove; i++ {
				removeDir := fmt.Sprintf("%s/%s", remotePath, backups[i])
				log.Info("Removing old backup", "dir", removeDir)
				sshCmd := fmt.Sprintf(`ssh %s "rm -rf %s"`, remoteHost, removeDir)
				if err := runShellCommand(sshCmd); err != nil {
					log.Error("Failed to remove old backup", "dir", removeDir, "err", err)
					continue
				}
			}
		}
	} else {
		entries, err := os.ReadDir(backupDir)
		if err != nil {
			log.Error("Error reading backup directory for cleanup", "dir", backupDir, "err", err)
			return
		}

		var backups []string
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasSuffix(entry.Name(), ".inprogress") {
				backups = append(backups, entry.Name())
			}
		}

		if len(backups) > maxBackups {
			toRemove := len(backups) - maxBackups
			for i := 0; i < toRemove; i++ {
				removeDir := filepath.Join(backupDir, backups[i])

				log.Info("Removing old backup", "dir", removeDir)
				if err := os.RemoveAll(removeDir); err != nil {
					log.Error("Failed to remove old backup", "dir", removeDir, "err", err)
					continue
				}
			}
		}
	}
}

// cleanupStaleInprogressDirs removes stale in-progress directories.
// It supports both local and remote targets.
func cleanupStaleInprogressDirs(target string) {
	if isRemotePath(target) {
		// Remote cleanup
		remoteHost, remotePath := parseRemotePath(target)

		// Find all remote in-progress directories
		sshCmd := fmt.Sprintf(`ssh %s "find %s -type d -name '*.inprogress'"`, remoteHost, remotePath)
		output, err := runCmdGetOutputString(sshCmd)
		if err != nil {
			log.Error("Error finding in-progress directories on remote host", "err", err)
			return
		}

		// Iterate over remote directories
		directories := strings.Split(strings.TrimSpace(output), "\n")
		for _, dir := range directories {
			if cfg.DeleteStaleInprogressDirs {
				removeRemoteDir(remoteHost, dir)
			} else {
				log.Warn("Stale in-progress directory found", "dir", dir)
			}
		}
	} else {
		// Local cleanup
		entries, err := os.ReadDir(target)
		if err != nil {
			log.Error("Error reading backup directory for cleanup", "dir", target, "err", err)
			return
		}

		// Iterate over local directories
		for _, entry := range entries {
			if entry.IsDir() && strings.Contains(entry.Name(), ".inprogress.") {
				dirPath := filepath.Join(target, entry.Name())
				if cfg.DeleteStaleInprogressDirs {
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
