package session

import (
	"slices"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/requestenv"
)

func TestNewRecordedSession(t *testing.T) {
	pcm := []byte{1, 2}
	s, err := NewRecordedSession(7, nil, pcm)
	if err != nil {
		t.Fatalf("NewRecordedSession failed: %v", err)
	}
	if s.ID() != 7 {
		t.Fatalf("ID = %d, want 7", s.ID())
	}
	if &s.PCM()[0] != &pcm[0] {
		t.Fatal("NewRecordedSession copied the PCM slice")
	}
	if s.Duration() != time.Second/16000 {
		t.Fatalf("duration = %v, want %v", s.Duration(), time.Second/16000)
	}
}

func TestSessionEnvironmentAndOrigin(t *testing.T) {
	environment := map[string]string{
		requestenv.SessionIDName: "session",
		"DISPLAY":                ":1",
	}
	s := NewSession(7, "session", environment)
	environment["DISPLAY"] = ":2"

	got := s.Environment()
	if !slices.Contains(got, "DISPLAY=:1") {
		t.Fatalf("session environment = %v, want DISPLAY=:1", got)
	}
	if s.OriginID() != "session" {
		t.Fatalf("OriginID = %q, want session", s.OriginID())
	}
}

func TestNewRecordedSessionRejectsInvalidPCM(t *testing.T) {
	if _, err := NewRecordedSession(7, nil, []byte{1}); err == nil {
		t.Fatal("NewRecordedSession accepted an odd PCM byte count")
	}
}
