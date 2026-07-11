// Package runtimedir manages daemon runtime files and paths.
package runtimedir

import (
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
)

// PrepareDir creates and cleans the runtime directory structure.
func PrepareDir(runtimeDir string, logger *log.Logger) error {
	if err := ensureOwnedDir(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("not usable runtime directory: %w", err)
	}
	empty, err := cleanTranscriptDir(runtimeDir)
	if err != nil {
		return err
	}
	if !empty {
		logger.Printf("transcript directory is not empty after cleanup")
	}
	return nil
}

func ensureOwnedDir(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() {
			return errors.New("not a directory")
		}
		if !ownedByCurrentUser(info) {
			return errors.New("not owned by current user")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to inspect: %w", err)
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("failed to create: %w", err)
	}
	info, err = os.Lstat(path)
	if err != nil {
		return fmt.Errorf("created but failed to inspect: %w", err)
	}
	if !info.IsDir() {
		return errors.New("created but not a directory")
	}
	if !ownedByCurrentUser(info) {
		return errors.New("created but not owned by current user")
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(stat.Uid) == os.Getuid()
}
