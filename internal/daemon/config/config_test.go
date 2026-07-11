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
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinDuration != 1250*time.Millisecond {
		t.Fatalf("MinDuration = %v, want 1.25s", cfg.MinDuration)
	}
}
