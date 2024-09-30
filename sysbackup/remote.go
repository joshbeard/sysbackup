package sysbackup

import (
	"log"
	"os/exec"
	"strings"
)

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
