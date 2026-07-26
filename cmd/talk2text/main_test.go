package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/edmonl/talk2text/cmd/talk2text/flags"
	"github.com/edmonl/talk2text/internal/daemon/config"
)

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := exitFailure
	err := run([]string{"unknown"}, &stdout, &stderr, &exitCode)
	if err == nil {
		t.Fatal("run succeeded with unknown subcommand")
	}
	if exitCode != exitUsage {
		t.Fatalf("exitCode = %d, want %d", exitCode, exitUsage)
	}
	if !strings.Contains(err.Error(), "unknown argument unknown") {
		t.Fatalf("err = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunRejectsNonHelpGlobalFlagWithoutUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := exitFailure
	err := run([]string{"--runtime-dir", "/tmp/talk2text-test"}, &stdout, &stderr, &exitCode)
	if err == nil {
		t.Fatal("run succeeded with non-help global flag")
	}
	if exitCode != exitUsage {
		t.Fatalf("exitCode = %d, want %d", exitCode, exitUsage)
	}
	if !strings.Contains(err.Error(), "flag provided but not defined: -runtime-dir") {
		t.Fatalf("err = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := exitFailure
	err := run([]string{"-h"}, &stdout, &stderr, &exitCode)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if exitCode != exitOk {
		t.Fatalf("exitCode = %d, want %d", exitCode, exitOk)
	}
	if !strings.Contains(stdout.String(), "talk2text <subcommand> [subcommand flags]") {
		t.Fatalf("stdout missing usage:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestParseDaemonFlags(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	exitCode := exitFailure
	err = parseArgs([]string{
		"--runtime-dir", "/tmp/talk2text-test",
		"--whisper-endpoint", "http://127.0.0.1:9090/inference",
		"--out-cmd", "/tmp/out",
		"--notify-cmd", "/tmp/notify",
		"--http-listen", "127.0.0.1:8081",
	}, flags.NewDaemonFlags(&cfg, io.Discard), &exitCode)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeDir != "/tmp/talk2text-test" {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
	if cfg.WhisperEndpoint != "http://127.0.0.1:9090/inference" {
		t.Fatalf("WhisperEndpoint = %q", cfg.WhisperEndpoint)
	}
	if cfg.OutCmd != "/tmp/out" {
		t.Fatalf("OutCmd = %q", cfg.OutCmd)
	}
	if cfg.NotifyCmd != "/tmp/notify" {
		t.Fatalf("NotifyCmd = %q", cfg.NotifyCmd)
	}
	if cfg.HTTPListen != "127.0.0.1:8081" {
		t.Fatalf("HTTPListen = %q", cfg.HTTPListen)
	}
}

func TestParseClientFlagsRejectsExtraArgument(t *testing.T) {
	var runtimeDir string
	exitCode := exitFailure
	err := parseArgs([]string{"--runtime-dir", "/tmp/talk2text-test", "extra"}, flags.NewClientFlags("start", &runtimeDir, io.Discard), &exitCode)
	if err == nil {
		t.Fatal("parseArgs succeeded with extra argument")
	}
	if exitCode != exitUsage {
		t.Fatalf("exitCode = %d, want %d", exitCode, exitUsage)
	}
	if !strings.Contains(err.Error(), "unknown argument extra") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRuntimeDirUsesExplicitValue(t *testing.T) {
	runtimeDir, err := resolveRuntimeDir("/tmp/talk2text-test")
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDir != "/tmp/talk2text-test" {
		t.Fatalf("runtimeDir = %q", runtimeDir)
	}
}

func TestResolveRuntimeDirUsesExistingXDG(t *testing.T) {
	xdgRuntimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", xdgRuntimeDir)
	t.Setenv("TMPDIR", "")

	runtimeDir, err := resolveRuntimeDir("")
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDir != filepath.Join(xdgRuntimeDir, "talk2text") {
		t.Fatalf("runtimeDir = %q", runtimeDir)
	}
}

func TestResolveRuntimeDirRejectsMissingXDGWithoutFallingThrough(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("XDG_RUNTIME_DIR", missing)
	t.Setenv("TMPDIR", t.TempDir())

	_, err := resolveRuntimeDir("")
	if err == nil {
		t.Fatal("resolveRuntimeDir succeeded with missing XDG_RUNTIME_DIR")
	}
	if !strings.Contains(err.Error(), "XDG_RUNTIME_DIR must be an existing directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRuntimeDirRejectsRelativeXDGWithoutFallingThrough(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "relative-run")
	t.Setenv("TMPDIR", "/tmp")

	_, err := resolveRuntimeDir("")
	if err == nil {
		t.Fatal("resolveRuntimeDir succeeded with relative XDG_RUNTIME_DIR")
	}
	if !strings.Contains(err.Error(), "XDG_RUNTIME_DIR must be absolute") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRuntimeDirUsesExistingTMPDIR(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", tmpDir)

	runtimeDir, err := resolveRuntimeDir("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmpDir, "run-"+strconv.Itoa(os.Getuid()), "talk2text")
	if runtimeDir != want {
		t.Fatalf("runtimeDir = %q, want %q", runtimeDir, want)
	}
}

func TestResolveRuntimeDirRejectsMissingTMPDIRWithoutFallingThrough(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", missing)

	_, err := resolveRuntimeDir("")
	if err == nil {
		t.Fatal("resolveRuntimeDir succeeded with missing TMPDIR")
	}
	if !strings.Contains(err.Error(), "TMPDIR must be an existing directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRuntimeDirRejectsRelativeTMPDIR(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "relative-tmp")

	_, err := resolveRuntimeDir("")
	if err == nil {
		t.Fatal("resolveRuntimeDir succeeded with relative TMPDIR")
	}
	if !strings.Contains(err.Error(), "TMPDIR must be absolute") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRuntimeDirUsesExistingTmpFallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "")

	runtimeDir, err := resolveRuntimeDir("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp", "run-"+strconv.Itoa(os.Getuid()), "talk2text")
	if runtimeDir != want {
		t.Fatalf("runtimeDir = %q, want %q", runtimeDir, want)
	}
}
