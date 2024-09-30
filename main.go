package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/log"
	"github.com/joshbeard/sysbackup/sysbackup"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	configFile   string
	cfg          sysbackup.Config
	generateFlag bool
	ctx          = context.Background()
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "sysbackup [flags] [source] [target]",
		Short:   "Rsync-based incremental backup tool",
		Example: strings.TrimPrefix(examples, "\n"),
		Long:    fmt.Sprintf("sysbackup v%s\n", fullVersion) + strings.TrimPrefix(about, "\n"),
		Version: fullVersion,
		Run: func(cmd *cobra.Command, args []string) {
			if generateFlag {
				out, err := cfg.Generate()
				if err != nil {
					log.Error("Failed to generate config", "err", err)
					os.Exit(1)
				}

				fmt.Println(out)
				os.Exit(0)
			}

			setupConfig()
			setupLogging()

			if len(args) == 1 || len(args) > 2 {
				log.Info("FAILED: Invalid number of arguments: %d", len(args))
				log.Info("Usage: %s", cmd.Use)

				os.Exit(1)
			}

			if len(args) == 2 {
				cfg.Source = args[0]
				cfg.Target = args[1]
			}

			sysbackup.Run(cfg)
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

	cmd.PersistentFlags().IntVar(&cfg.MaxBackups,
		"max-backups", 0,
		"Maximum number of backups to keep (0 for no limit)")

	cmd.PersistentFlags().IntVar(&cfg.MaxAge,
		"max-age", 0,
		"Maximum age of backups to keep (in days)")

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

	cmd.PersistentFlags().BoolVarP(&generateFlag, "gen-config", "g", false,
		"Generate a default config file and exit")

	viper.BindPFlag("rsync_args", cmd.PersistentFlags().Lookup("rsync-args"))
	viper.BindPFlag("source", cmd.PersistentFlags().Lookup("source"))
	viper.BindPFlag("target", cmd.PersistentFlags().Lookup("target"))
	viper.BindPFlag("filter_file", cmd.PersistentFlags().Lookup("filter-file"))
	viper.BindPFlag("max_backups", cmd.PersistentFlags().Lookup("max-backups"))
	viper.BindPFlag("max_age", cmd.PersistentFlags().Lookup("max-age"))
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

func setupConfig() error {
	if configFile == "" {
		configFile = sysbackup.FindConfigFile()
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
