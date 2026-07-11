package runtimedir

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const socketName = "daemon.sock"

// SocketPath returns the daemon IPC socket path inside runtimeDir.
func SocketPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, socketName)
}

// PrepareSocket returns a usable daemon IPC socket path inside runtimeDir.
func PrepareSocket(runtimeDir string) (string, error) {
	socketPath := SocketPath(runtimeDir)
	if info, err := os.Lstat(socketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return socketPath, nil
		}
		return "", fmt.Errorf("failed to inspect socket path: %w", err)
	} else if info.Mode()&os.ModeSocket == 0 {
		return "", fmt.Errorf("socket path exists but is not a socket")
	}

	if conn, err := net.DialTimeout("unix", socketPath, 250*time.Millisecond); err == nil {
		conn.Close()
		return "", errors.New("socket is in use")
	}

	if err := os.Remove(socketPath); err != nil {
		return "", fmt.Errorf("failed to remove stale socket: %w", err)
	}

	return socketPath, nil
}
