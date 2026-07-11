package client

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/daemon"
	"github.com/edmonl/talk2text/internal/daemon/config"
)

func TestPrintStatusFormatsDurations(t *testing.T) {
	resp := daemon.Status{
		State:      "off",
		NextClipID: 1,
		Config: &config.Config{
			RuntimeDir:            "/tmp/talk2text-test",
			WhisperEndpoint:       "http://127.0.0.1:8080/inference",
			MinDuration:           500 * time.Millisecond,
			MaxDuration:           100 * time.Second,
			WarmRetention:         15 * time.Second,
			WhisperConnectTimeout: time.Second,
			WhisperRequestTimeout: 10 * time.Second,
		},
	}
	var out bytes.Buffer
	printStatus(&out, resp)
	text := out.String()
	for _, want := range []string{
		"min_duration: 500ms",
		"max_duration: 1m40s",
		"warm_retention: 15s",
		"whisper_connect_timeout: 1s",
		"whisper_request_timeout: 10s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
}
