package notifier

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/requestenv"
)

func TestNotificationCommandContract(t *testing.T) {
	command := filepath.Join(t.TempDir(), "notify")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n%s\\n' \"$#\" \"$1\" \"$TALK2TEXT_NOTIFY_LEVEL\" \"$TALK2TEXT_NOTIFY_CODE\" > \"$NOTIFY_TEST_OUTPUT\"\n"
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(notifyLevelEnv, "stale")
	t.Setenv(notifyCodeEnv, "stale")

	for _, tc := range []struct {
		name  string
		emit  func(*Notifier)
		level string
		code  string
	}{
		{
			name: "info",
			emit: func(n *Notifier) {
				n.Info("record-start", "Recording clip 7", nil)
			},
			level: "info",
			code:  "record-start",
		},
		{
			name: "error",
			emit: func(n *Notifier) {
				n.Error("whisper", "Transcribing clip 7 failed", nil)
			},
			level: "error",
			code:  "whisper",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "notification")
			t.Setenv("NOTIFY_TEST_OUTPUT", output)
			n := New(context.Background(), command, log.New(io.Discard, "", 0))

			tc.emit(n)

			want := "1\n"
			if tc.level == "info" {
				want += "Recording clip 7\n"
			} else {
				want += "Transcribing clip 7 failed\n"
			}
			want += tc.level + "\n" + tc.code + "\n"
			deadline := time.Now().Add(time.Second)
			var got string
			for time.Now().Before(deadline) {
				raw, err := os.ReadFile(output)
				if err == nil {
					got = string(raw)
					if got == want {
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("notification command contract = %q, want %q", got, want)
		})
	}
}

func TestNotificationCommandOverlaysClipEnvironmentBeforeMetadata(t *testing.T) {
	run := t.TempDir()
	command := filepath.Join(run, "notify")
	output := filepath.Join(run, "output")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$DISPLAY\" \"$TALK2TEXT_OUTPUT_TARGET\" \"$TALK2TEXT_NOTIFY_LEVEL\" > \"$NOTIFY_TEST_OUTPUT\"\n"
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFY_TEST_OUTPUT", output)
	t.Setenv("DISPLAY", ":daemon")
	t.Setenv(notifyLevelEnv, "stale")
	n := New(context.Background(), command, log.New(io.Discard, "", 0))

	n.Info("record-start", "Recording clip 7", []string{
		"DISPLAY=:request",
		requestenv.OutputTargetName + "=mobile",
		notifyLevelEnv + "=request",
	})

	want := ":request\nmobile\ninfo\n"
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(output)
		if err == nil && string(raw) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := os.ReadFile(output)
	t.Fatalf("notification environment = %q, want %q", raw, want)
}
