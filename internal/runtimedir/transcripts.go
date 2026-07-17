package runtimedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const transcriptsDir = "transcripts"

// TranscriptsDir returns the transcripts directory inside runtimeDir.
func TranscriptsDir(runtimeDir string) string {
	return filepath.Join(runtimeDir, transcriptsDir)
}

// WriteTranscript writes text to clipID's transcript file and returns its path.
func WriteTranscript(runtimeDir string, clipID int, text string) (string, error) {
	path := filepath.Join(runtimeDir, transcriptsDir, strconv.Itoa(clipID)+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func cleanTranscriptDir(runtimeDir string) (bool, error) {
	path := TranscriptsDir(runtimeDir)
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

// ProcessTranscriptFiles calls process for each regular clip-ID transcript file.
func ProcessTranscriptFiles(runtimeDir string, process func(name string, clipID int)) error {
	entries, err := os.ReadDir(TranscriptsDir(runtimeDir))
	if err != nil {
		return fmt.Errorf("failed to read transcript directory: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("failed to inspect transcript file %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		clipID := transcriptClipID(entry.Name())
		if clipID == 0 {
			continue
		}
		process(entry.Name(), clipID)
	}
	return nil
}

func transcriptClipID(name string) int {
	value, ok := strings.CutSuffix(name, ".txt")
	if !ok {
		return 0
	}
	clipID, _ := strconv.Atoi(value)
	if clipID < 1 {
		return 0
	}
	return clipID
}
