package runtimedir

import (
	"bytes"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPrepareDirCreatesRuntimeAndTranscriptDirs(t *testing.T) {
	run := filepath.Join(t.TempDir(), "run")
	if err := PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	assertDirMode(t, run, 0o700)
	assertDirMode(t, filepath.Join(run, transcriptsDir), 0o700)
}

func TestPrepareDirCreatesAndCleansOnlyRegularTranscriptFiles(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "run")
	if err := PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(run, transcriptsDir, "stale.txt")
	keptDir := filepath.Join(run, transcriptsDir, "kept")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(keptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	if err := PrepareDir(run, logger); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale regular file still exists or stat failed differently: %v", err)
	}
	if info, err := os.Stat(keptDir); err != nil || !info.IsDir() {
		t.Fatalf("subdirectory was not preserved: %v", err)
	}
	if !strings.Contains(logs.String(), "transcript directory is not empty after cleanup") {
		t.Fatalf("logs missing non-empty transcript directory warning:\n%s", logs.String())
	}
}

func TestCleanTranscriptDirReportsEmpty(t *testing.T) {
	run := t.TempDir()
	transcriptDir := filepath.Join(run, transcriptsDir)
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDir, "stale.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty, err := cleanTranscriptDir(run)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Fatal("cleanTranscriptDir reported non-empty after removing only regular files")
	}
}

func TestCleanTranscriptDirReportsNonEmptyWhenNonRegularEntriesRemain(t *testing.T) {
	run := t.TempDir()
	transcriptDir := filepath.Join(run, transcriptsDir)
	if err := os.MkdirAll(filepath.Join(transcriptDir, "kept"), 0o700); err != nil {
		t.Fatal(err)
	}
	empty, err := cleanTranscriptDir(run)
	if err != nil {
		t.Fatal(err)
	}
	if empty {
		t.Fatal("cleanTranscriptDir reported empty with subdirectory remaining")
	}
}

func TestPrepareDirRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDir(path, nil); err == nil {
		t.Fatalf("PrepareDir succeeded for regular file")
	}
}

func TestPrepareDirRejectsRegularFileAtTranscriptPath(t *testing.T) {
	run := t.TempDir()
	if err := os.WriteFile(filepath.Join(run, transcriptsDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDir(run, nil); err == nil {
		t.Fatalf("PrepareDir succeeded with regular file at transcript directory path")
	}
}

func TestWriteTranscriptWritesFile(t *testing.T) {
	run := t.TempDir()
	if err := PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	path, err := WriteTranscript(run, 42, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(run, transcriptsDir, "42.txt")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello world" {
		t.Fatalf("transcript = %q", string(raw))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestProcessTranscriptFilesReturnsClipIDFiles(t *testing.T) {
	run := t.TempDir()
	if err := PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	for clipID := 1; clipID <= 3; clipID++ {
		if _, err := WriteTranscript(run, clipID, strconv.Itoa(clipID)); err != nil {
			t.Fatal(err)
		}
	}

	got := make(map[string]int)
	if err := ProcessTranscriptFiles(run, func(name string, clipID int) {
		got[name] = clipID
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("processed files = %v, want three transcripts", got)
	}
	for clipID := 1; clipID <= 3; clipID++ {
		name := strconv.Itoa(clipID) + ".txt"
		if got[name] != clipID {
			t.Fatalf("processed clip ID for %s = %d, want %d", name, got[name], clipID)
		}
	}
}

func TestPruneTranscriptFilesPreservesIgnoredEntries(t *testing.T) {
	run := t.TempDir()
	if err := PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteTranscript(run, 1, "one"); err != nil {
		t.Fatal(err)
	}
	transcriptDir := filepath.Join(run, transcriptsDir)
	dirPath := filepath.Join(transcriptDir, "kept")
	linkPath := filepath.Join(transcriptDir, "link.txt")
	invalidPath := filepath.Join(transcriptDir, "notes.txt")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(transcriptDir, "1.txt"), linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, []byte("not a transcript"), 0o600); err != nil {
		t.Fatal(err)
	}

	var processed []string
	if err := ProcessTranscriptFiles(run, func(name string, _ int) {
		processed = append(processed, name)
	}); err != nil {
		t.Fatal(err)
	}
	if len(processed) != 1 || processed[0] != "1.txt" {
		t.Fatalf("processed files = %v, want [1.txt]", processed)
	}
	if info, err := os.Lstat(dirPath); err != nil || !info.IsDir() {
		t.Fatalf("directory was not preserved: info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was not preserved: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(invalidPath); err != nil {
		t.Fatalf("regular file without a clip ID was not preserved: %v", err)
	}
}

func TestPrepareSocketReturnsMissingSocketPath(t *testing.T) {
	run := t.TempDir()
	want := SocketPath(run)
	got, err := PrepareSocket(run)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("socket path = %q, want %q", got, want)
	}
	if _, err := os.Lstat(want); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PrepareSocket created socket path or stat failed differently: %v", err)
	}
}

func TestPrepareSocketRejectsNonSocket(t *testing.T) {
	run := t.TempDir()
	path := SocketPath(run)
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSocket(run); err == nil {
		t.Fatalf("PrepareSocket succeeded for non-socket file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "not a socket" {
		t.Fatalf("non-socket file was modified: %q", string(raw))
	}
}

func TestPrepareSocketRejectsLiveDaemon(t *testing.T) {
	run := t.TempDir()
	socketPath := SocketPath(run)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	_, err = PrepareSocket(run)
	if err == nil {
		t.Fatalf("PrepareSocket succeeded with live socket")
	}
}

func TestPrepareSocketRemovesStaleSocket(t *testing.T) {
	run := t.TempDir()
	path := SocketPath(run)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	socketPath, err := PrepareSocket(run)
	if err != nil {
		t.Fatal(err)
	}
	if socketPath != path {
		t.Fatalf("socket path = %q, want %q", socketPath, path)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket still exists or stat failed differently: %v", err)
	}
}

func assertDirMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
	}
}
