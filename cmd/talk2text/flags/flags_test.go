package flags

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/edmonl/talk2text/internal/client"
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
	var cfg client.Config
	fs := NewClientFlags("status", &cfg, io.Discard)
	if _, err := fs.Parse([]string{"--runtime-dir", "/tmp/talk2text-test"}); err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeDir != "/tmp/talk2text-test" {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
}

func TestStartFlagsBindAndDeduplicateSendEnvironment(t *testing.T) {
	var cfg client.Config
	fs := NewClientFlags("start", &cfg, io.Discard)
	if _, err := fs.Parse([]string{"--send-env", "SWAYSOCK", "--send-env", "SWAYSOCK"}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.SendEnv) != 1 || cfg.SendEnv[0] != "SWAYSOCK" {
		t.Fatalf("SendEnv = %v, want [SWAYSOCK]", cfg.SendEnv)
	}
}

func TestStopFlagsDoNotAcceptSendEnvironment(t *testing.T) {
	var cfg client.Config
	fs := NewClientFlags("stop", &cfg, io.Discard)
	if _, err := fs.Parse([]string{"--send-env", "SWAYSOCK"}); err == nil {
		t.Fatal("stop accepted --send-env")
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
		"address for accepting AMR-WB HTTP transcription requests",
		"additional request environment variable to allow",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage missing %q:\n%s", want, output)
		}
	}
}
