package runtimedir

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const transcriptsDir = "transcripts"

// WriteTranscript writes text to clipID's transcript file and returns its path.
func WriteTranscript(runtimeDir string, clipID int, text string) (string, error) {
	path := filepath.Join(runtimeDir, transcriptsDir, strconv.Itoa(clipID)+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func cleanTranscriptDir(runtimeDir string) (bool, error) {
	path := filepath.Join(runtimeDir, transcriptsDir)
	if err := ensureOwnedDir(path, 0o700); err != nil {
		return false, fmt.Errorf("not usable transcript directory: %w", err)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("failed to read transcript directory: %w", err)
	}
	empty := true
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			if err := os.Remove(filepath.Join(path, entry.Name())); err != nil {
				empty = false
			}
			continue
		}
		empty = false
	}
	return empty, nil
}
