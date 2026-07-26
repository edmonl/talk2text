// Package session tracks one daemon recording session.
package session

import (
	"bytes"
	"fmt"
	"time"

	"github.com/edmonl/talk2text/internal/whisper"
)

type Session struct {
	id       int
	buffer   *bytes.Buffer
	duration time.Duration
}

// NewSession creates an empty session.
func NewSession(id int) *Session {
	return &Session{
		id:     id,
		buffer: &bytes.Buffer{},
	}
}

// NewSessionWithPCM creates a session backed by pcm. The caller must not
// modify a non-nil pcm slice after passing it to NewSessionWithPCM.
func NewSessionWithPCM(id int, pcm []byte) (*Session, error) {
	duration, err := pcmDuration(pcm)
	if err != nil {
		return nil, err
	}
	s := NewSession(id)
	if pcm != nil {
		s.buffer = bytes.NewBuffer(pcm)
	}
	s.duration = duration
	return s, nil
}

func (s *Session) ID() int {
	return s.id
}

func (s *Session) PCM() []byte {
	return s.buffer.Bytes()
}

func (s *Session) Duration() time.Duration {
	return s.duration
}

func (s *Session) OnPCM(pcm []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.clear()
			err = fmt.Errorf("failed to receive pcm data: %v", r)
		}
	}()
	delta, err := pcmDuration(pcm)
	if err != nil {
		s.clear()
		return err
	}
	// bytes.Buffer.Write never throws but may panic.
	s.buffer.Write(pcm)
	s.duration += delta
	return nil
}

func (s *Session) clear() {
	s.buffer = &bytes.Buffer{}
	s.duration = 0
}

func pcmDuration(pcm []byte) (time.Duration, error) {
	l := len(pcm)
	if l%2 != 0 {
		return 0, fmt.Errorf("invalid pcm data: byte length %d is not a multiple of 2", l)
	}
	samples := l / 2
	return time.Duration(int64(samples) * int64(time.Second) / int64(whisper.SampleRateHz)), nil
}
