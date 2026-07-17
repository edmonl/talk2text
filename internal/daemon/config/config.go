// Package config defines daemon configuration defaults and validation.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultWhisperEndpoint = "http://127.0.0.1:8080/inference"
	minConfigDuration      = 10 * time.Millisecond
)

// Config is the effective daemon configuration shared through IPC status.
type Config struct {
	// RuntimeDir stores runtime files, including the daemon socket and transcripts.
	RuntimeDir string `json:"runtime_dir"`
	// WhisperEndpoint is the Whisper-compatible HTTP inference endpoint.
	WhisperEndpoint string `json:"whisper_endpoint"`
	// OutCmd is an optional command run after each completed clip.
	OutCmd string `json:"out_cmd,omitempty"`
	// NotifyCmd is an optional command used for user notifications.
	NotifyCmd string `json:"notify_cmd,omitempty"`
	// MinDuration is the shortest recording duration accepted for transcription; zero accepts every clip.
	MinDuration time.Duration `json:"min_duration"`
	// MaxDuration is the longest active recording duration before automatic stop; zero disables auto-stop.
	MaxDuration time.Duration `json:"max_duration"`
	// WarmRetention is how long the idle daemon keeps the audio stream open; zero closes it immediately.
	WarmRetention time.Duration `json:"warm_retention"`
	// TranscriptRetentionWindow retains recent clip IDs; zero disables runtime retention cleanup.
	TranscriptRetentionWindow int `json:"transcript_retention_window"`
	// RecordInputDevice selects the audio input device, or empty for default.
	RecordInputDevice string `json:"record_input_device,omitempty"`
	// WhisperConnectTimeout limits the Whisper endpoint connection phase; zero disables the connect timeout.
	WhisperConnectTimeout time.Duration `json:"whisper_connect_timeout"`
	// WhisperRequestTimeout limits the Whisper HTTP request after recording stops; zero disables the request timeout.
	WhisperRequestTimeout time.Duration `json:"whisper_request_timeout"`
}

// DefaultConfig returns configuration from environment variables and built-in defaults.
func DefaultConfig() (Config, error) {
	minDuration, err := envDuration("TALK2TEXT_MIN_DURATION", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	maxDuration, err := envDuration("TALK2TEXT_MAX_DURATION", 100*time.Second)
	if err != nil {
		return Config{}, err
	}
	warmRetention, err := envDuration("TALK2TEXT_WARM_RETENTION", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	transcriptRetentionWindow, err := envInt("TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW", 100)
	if err != nil {
		return Config{}, err
	}
	whisperConnectTimeout, err := envDuration("TALK2TEXT_WHISPER_CONNECT_TIMEOUT", time.Second)
	if err != nil {
		return Config{}, err
	}
	whisperRequestTimeout, err := envDuration("TALK2TEXT_WHISPER_REQUEST_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		WhisperEndpoint:           defaultWhisperEndpoint,
		MinDuration:               minDuration,
		MaxDuration:               maxDuration,
		WarmRetention:             warmRetention,
		TranscriptRetentionWindow: transcriptRetentionWindow,
		RecordInputDevice:         os.Getenv("TALK2TEXT_RECORD_INPUT_DEVICE"),
		WhisperConnectTimeout:     whisperConnectTimeout,
		WhisperRequestTimeout:     whisperRequestTimeout,
	}
	return cfg, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	integer, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return integer, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration string such as 500ms, 15s, or 1m40s: %w", name, err)
	}
	return duration, nil
}

// ValidateConfig checks configuration values required before daemon startup.
func ValidateConfig(cfg Config) error {
	if cfg.WhisperEndpoint == "" {
		return errors.New("whisper endpoint must not be empty")
	}
	if err := validateConfigDuration("minimum duration", cfg.MinDuration); err != nil {
		return err
	}
	if err := validateConfigDuration("maximum duration", cfg.MaxDuration); err != nil {
		return err
	}
	if err := validateConfigDuration("warm retention", cfg.WarmRetention); err != nil {
		return err
	}
	if cfg.TranscriptRetentionWindow < 0 {
		return errors.New("transcript retention window must not be negative")
	}
	if err := validateConfigDuration("whisper connect timeout", cfg.WhisperConnectTimeout); err != nil {
		return err
	}
	if err := validateConfigDuration("whisper request timeout", cfg.WhisperRequestTimeout); err != nil {
		return err
	}
	if cfg.MaxDuration > 0 && cfg.MaxDuration <= cfg.MinDuration {
		return errors.New("maximum duration must be greater than minimum duration")
	}
	return nil
}

func validateConfigDuration(name string, duration time.Duration) error {
	if duration < 0 {
		return fmt.Errorf("%s must not be negative", name)
	}
	if duration > 0 && duration < minConfigDuration {
		return fmt.Errorf("%s must be 0s or at least %s", name, minConfigDuration)
	}
	return nil
}
