package client

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/daemon"
	"github.com/edmonl/talk2text/internal/daemon/config"
	"github.com/edmonl/talk2text/internal/requestenv"
)

func TestPrintStatusFormatsDurations(t *testing.T) {
	resp := daemon.Status{
		State:      "off",
		NextClipID: 1,
		Config: &config.Config{
			RuntimeDir:                "/tmp/talk2text-test",
			WhisperEndpoint:           "http://127.0.0.1:8080/inference",
			HTTPListen:                "127.0.0.1:8081",
			AllowClientEnv:            []string{"SWAYSOCK", "CUSTOM"},
			MinDuration:               500 * time.Millisecond,
			MaxDuration:               100 * time.Second,
			StopDelay:                 250 * time.Millisecond,
			WarmRetention:             15 * time.Second,
			TranscriptRetentionWindow: 100,
			WhisperConnectTimeout:     time.Second,
			WhisperRequestTimeout:     10 * time.Second,
		},
	}
	var out bytes.Buffer
	printStatus(&out, resp)
	text := out.String()
	for _, want := range []string{
		"min_duration: 500ms",
		"http_listen: 127.0.0.1:8081",
		"allow_client_env: SWAYSOCK,CUSTOM",
		"max_duration: 1m40s",
		"stop_delay: 250ms",
		"warm_retention: 15s",
		"transcript_retention_window: 100",
		"whisper_connect_timeout: 1s",
		"whisper_request_timeout: 10s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
}

func TestBuildRequestUsesCommandAutomaticEnvironmentSubsets(t *testing.T) {
	t.Setenv(requestenv.SessionIDName, "session")
	t.Setenv(requestenv.XDGSessionIDName, "3")
	t.Setenv("DISPLAY", ":1")
	t.Setenv(requestenv.OutputTargetName, "global-target")
	t.Setenv("SWAYSOCK", "socket")

	start, err := buildRequest("start", []string{"SWAYSOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if start.Env == nil {
		t.Fatal("start environment is nil")
	}
	if start.Env["DISPLAY"] != ":1" || start.Env["SWAYSOCK"] != "socket" {
		t.Fatalf("start environment = %v", start.Env)
	}
	if _, ok := start.Env[requestenv.OutputTargetName]; ok {
		t.Fatalf("start automatically sent %s", requestenv.OutputTargetName)
	}

	stop, err := buildRequest("stop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stop.Env == nil {
		t.Fatal("stop environment is nil")
	}
	if len(stop.Env) != 2 || stop.Env[requestenv.SessionIDName] != "session" || stop.Env[requestenv.XDGSessionIDName] != "3" {
		t.Fatalf("stop environment = %v, want only session identifiers", stop.Env)
	}

	status, err := buildRequest("status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.Env != nil {
		t.Fatalf("status environment = %v, want nil", status.Env)
	}
}

func TestBuildRequestOmitsUnsetExplicitEnvironmentVariable(t *testing.T) {
	const absent = "TALK2TEXT_TEST_DEFINITELY_ABSENT_7D65C2"
	request, err := buildRequest("start", []string{absent})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := request.Env[absent]; ok {
		t.Fatalf("buildRequest included unset environment variable %s", absent)
	}
}

func TestBuildRequestValidatesAutomaticEnvironmentValues(t *testing.T) {
	t.Setenv(requestenv.DisplayName, strings.Repeat("x", 1<<20))
	if _, err := buildRequest("start", nil); err == nil || !strings.Contains(err.Error(), "invalid environment variable DISPLAY") {
		t.Fatalf("buildRequest error = %v, want invalid automatic value error", err)
	}
}
