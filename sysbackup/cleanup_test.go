package sysbackup

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCleanupOldBackups(t *testing.T) {
	// Helper function to generate a list of backup dates based on days in the past
	generateBackupDates := func(days []int) []string {
		var backups []string
		for _, d := range days {
			backups = append(backups, time.Now().AddDate(0, 0, -d).Format("2006-01-02"))
		}
		return backups
	}

	tests := []struct {
		name            string
		maxBackups      int
		maxAge          int
		existingBackups []string
		expectedBackups []string
	}{
		{
			name:            "No cleanup when maxBackups and maxAge are 0",
			maxBackups:      0,
			maxAge:          0,
			existingBackups: generateBackupDates([]int{3, 2, 1}),
			expectedBackups: generateBackupDates([]int{3, 2, 1}),
		},
		{
			name:            "Remove oldest backups based on maxBackups",
			maxBackups:      2,
			maxAge:          0,
			existingBackups: generateBackupDates([]int{3, 2, 1}),
			expectedBackups: generateBackupDates([]int{2, 1}),
		},
		{
			name:            "Remove backups older than maxAge",
			maxBackups:      0,
			maxAge:          2,
			existingBackups: generateBackupDates([]int{3, 2, 1}),
			expectedBackups: generateBackupDates([]int{1}),
		},
		{
			name:            "Remove backups based on both maxBackups and maxAge",
			maxBackups:      2,
			maxAge:          2,
			existingBackups: generateBackupDates([]int{3, 2, 1}),
			expectedBackups: generateBackupDates([]int{1}),
		},
		{
			name:            "Keep only 1 backup when both maxBackups and maxAge would remove all",
			maxBackups:      1,
			maxAge:          1,
			existingBackups: generateBackupDates([]int{5, 4, 3, 2, 1}),
			expectedBackups: generateBackupDates([]int{1}),
		},
		{
			name:            "Only maxBackups limit applies when maxAge is 0",
			maxBackups:      3,
			maxAge:          0,
			existingBackups: generateBackupDates([]int{5, 4, 3, 2, 1}),
			expectedBackups: generateBackupDates([]int{3, 2, 1}),
		},
		{
			name:            "Only maxAge applies when maxBackups is 0",
			maxBackups:      0,
			maxAge:          5,
			existingBackups: generateBackupDates([]int{7, 6, 5, 4, 3, 2, 1}),
			expectedBackups: generateBackupDates([]int{4, 3, 2, 1}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the backups
			backupDir := t.TempDir()
			// Create the existing backups
			for _, backup := range tt.existingBackups {
				err := os.Mkdir(filepath.Join(backupDir, backup), fs.ModePerm)
				assert.NoError(t, err)
			}

			bk := App{
				cfg: Config{
					Target:     backupDir,
					MaxAge:     tt.maxAge,
					MaxBackups: tt.maxBackups,
				}}

			bk.cleanupOldBackups()

			// Read back the remaining backups
			entries, err := os.ReadDir(backupDir)
			assert.NoError(t, err)

			var remainingBackups []string
			for _, entry := range entries {
				if entry.IsDir() {
					remainingBackups = append(remainingBackups, entry.Name())
				}
			}

			assert.ElementsMatch(t, tt.expectedBackups, remainingBackups)
		})
	}
}
