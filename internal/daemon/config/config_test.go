package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigReturnsEnvDurationParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "invalid", value: "nope", want: "TALK2TEXT_MIN_DURATION must be a duration string"},
		{name: "unitless", value: "1", want: "TALK2TEXT_MIN_DURATION must be a duration string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TALK2TEXT_MIN_DURATION", tc.value)
			_, err := DefaultConfig()
			if err == nil {
				t.Fatal("DefaultConfig succeeded with invalid duration")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDefaultConfigReturnsEnvIntParseError(t *testing.T) {
	t.Setenv("TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW", "many")
	_, err := DefaultConfig()
	if err == nil {
		t.Fatal("DefaultConfig succeeded with invalid transcript retention window")
	}
	if !strings.Contains(err.Error(), "TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW must be an integer") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateConfigReturnsDurationErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update func(*Config)
		want   string
	}{
		{
			name: "negative",
			update: func(cfg *Config) {
				cfg.MinDuration = -time.Second
			},
			want: "minimum duration must not be negative",
		},
		{
			name: "too short",
			update: func(cfg *Config) {
				cfg.MinDuration = time.Millisecond
			},
			want: "minimum duration must be 0s or at least 10ms",
		},
		{
			name: "negative stop delay",
			update: func(cfg *Config) {
				cfg.StopDelay = -time.Second
			},
			want: "stop delay must not be negative",
		},
		{
			name: "negative transcript retention window",
			update: func(cfg *Config) {
				cfg.TranscriptRetentionWindow = -1
			},
			want: "transcript retention window must not be negative",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatal(err)
			}
			tc.update(&cfg)
			err = ValidateConfig(cfg)
			if err == nil {
				t.Fatal("ValidateConfig succeeded with invalid duration")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDefaultConfigUsesEnvDuration(t *testing.T) {
	t.Setenv("TALK2TEXT_MIN_DURATION", "1250ms")
	t.Setenv("TALK2TEXT_STOP_DELAY", "375ms")
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinDuration != 1250*time.Millisecond {
		t.Fatalf("MinDuration = %v, want 1.25s", cfg.MinDuration)
	}
	if cfg.StopDelay != 375*time.Millisecond {
		t.Fatalf("StopDelay = %v, want 375ms", cfg.StopDelay)
	}
}

func TestDefaultConfigUsesDefaultStopDelay(t *testing.T) {
	t.Setenv("TALK2TEXT_STOP_DELAY", "")
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StopDelay != 250*time.Millisecond {
		t.Fatalf("StopDelay = %v, want 250ms", cfg.StopDelay)
	}
}

func TestDefaultConfigUsesTranscriptRetentionWindow(t *testing.T) {
	t.Setenv("TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW", "25")
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TranscriptRetentionWindow != 25 {
		t.Fatalf("TranscriptRetentionWindow = %d, want 25", cfg.TranscriptRetentionWindow)
	}
}

func TestDefaultConfigUsesDefaultTranscriptRetentionWindow(t *testing.T) {
	t.Setenv("TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW", "")
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TranscriptRetentionWindow != 100 {
		t.Fatalf("TranscriptRetentionWindow = %d, want 100", cfg.TranscriptRetentionWindow)
	}
}
