package session

import (
	"testing"
	"time"
)

func TestNewSessionWithPCM(t *testing.T) {
	pcm := []byte{1, 2}
	s, err := NewSessionWithPCM(7, pcm)
	if err != nil {
		t.Fatalf("NewSessionWithPCM failed: %v", err)
	}
	if s.ID() != 7 {
		t.Fatalf("ID = %d, want 7", s.ID())
	}
	if &s.PCM()[0] != &pcm[0] {
		t.Fatal("NewSessionWithPCM copied the PCM slice")
	}
	if s.Duration() != time.Second/16000 {
		t.Fatalf("duration = %v, want %v", s.Duration(), time.Second/16000)
	}
}

func TestNewSessionWithPCMRejectsInvalidPCM(t *testing.T) {
	if _, err := NewSessionWithPCM(7, []byte{1}); err == nil {
		t.Fatal("NewSessionWithPCM accepted an odd PCM byte count")
	}
}
