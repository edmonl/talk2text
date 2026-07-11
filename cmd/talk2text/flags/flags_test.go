package flags

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/edmonl/talk2text/internal/daemon/config"
)

func TestGlobalUsageIncludesSubcommands(t *testing.T) {
	var out bytes.Buffer
	NewGlobalFlags(&out).Usage()
	output := out.String()
	for _, want := range []string{
		"A push-to-talk speech-to-text tool.",
		"talk2text <subcommand> [subcommand flags]",
		"daemon  run background daemon",
		"start   start recording",
		"stop    stop recording",
		"status  print daemon state and configuration",
		"-h/-help",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage missing %q:\n%s", want, output)
		}
	}
}

func TestClientFlagsBindRuntimeDir(t *testing.T) {
	var runtimeDir string
	fs := NewClientFlags("status", &runtimeDir, io.Discard)
	if _, err := fs.Parse([]string{"--runtime-dir", "/tmp/talk2text-test"}); err != nil {
		t.Fatal(err)
	}
	if runtimeDir != "/tmp/talk2text-test" {
		t.Fatalf("runtimeDir = %q", runtimeDir)
	}
}

func TestDaemonFlagsBindConfig(t *testing.T) {
	cfg := config.Config{WhisperEndpoint: "http://127.0.0.1:8080/inference"}
	fs := NewDaemonFlags(&cfg, io.Discard)
	if _, err := fs.Parse([]string{
		"--runtime-dir", "/tmp/talk2text-test",
		"--whisper-endpoint", "http://127.0.0.1:9090/inference",
		"--out-cmd", "/tmp/out",
		"--notify-cmd", "/tmp/notify",
	}); err != nil {
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
}

func TestParseHelpPrintsUsage(t *testing.T) {
	var out bytes.Buffer
	fs := NewGlobalFlags(&out)
	if _, err := fs.Parse([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out.String(), "talk2text <subcommand> [subcommand flags]") {
		t.Fatalf("stdout missing usage:\n%s", out.String())
	}
}

func TestParseErrorDoesNotPrintUsage(t *testing.T) {
	var out bytes.Buffer
	fs := NewGlobalFlags(&out)
	if _, err := fs.Parse([]string{"--runtime-dir", "/tmp/talk2text-test"}); err == nil {
		t.Fatal("Parse succeeded")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestDaemonUsageIncludesFlagDescriptions(t *testing.T) {
	cfg := config.Config{WhisperEndpoint: "http://127.0.0.1:8080/inference"}
	var out bytes.Buffer
	NewDaemonFlags(&cfg, &out).Usage()
	output := out.String()
	for _, want := range []string{
		"talk2text daemon [flags]",
		"runtime directory for the daemon",
		"Whisper.cpp server HTTP endpoint",
		"command run after each completed clip",
		"command used to emit user notifications",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage missing %q:\n%s", want, output)
		}
	}
}
