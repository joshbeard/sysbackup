package sysbackup

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Source                    string `mapstructure:"source" yaml:"source"`
	Target                    string `mapstructure:"target" yaml:"target"`
	FilterFile                string `mapstructure:"filter_file" yaml:"filter_file"`
	PidFile                   string `mapstructure:"pid_file" yaml:"pid_file"`
	RsyncArgs                 string `mapstructure:"rsync_args" yaml:"rsync_args"`
	DateFormat                string `mapstructure:"date_format" yaml:"date_format"`
	MaxBackups                int    `mapstructure:"max_backups" yaml:"max_backups"`
	MaxAge                    int    `mapstructure:"max_age" yaml:"max_age"`
	DeleteStaleInprogressDirs bool   `mapstructure:"delete_stale_inprogress_dirs" yaml:"delete_stale_inprogress_dirs"`
	CheckFreeSpace            bool   `mapstructure:"check_free_space" yaml:"check_free_space"`
	EstimateBackupSize        bool   `mapstructure:"estimate_backup_size" yaml:"estimate_backup_size"`
	MinFreeSpace              int    `mapstructure:"min_free_space" yaml:"min_free_space"`
	CreateTarget              bool   `mapstructure:"create_target" yaml:"create_target"`
	LogLevel                  string `mapstructure:"log_level" yaml:"log_level"`
	LogRsync                  bool   `mapstructure:"log_rsync" yaml:"log_rsync"`
	genConfig                 bool
}

func (c Config) Generate() (string, error) {
	out, err := yaml.Marshal(c)
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

func FindConfigFile() string {
	for _, path := range configPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
