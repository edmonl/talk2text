// Package audiocapture opens microphone streams for talk2text.
package audiocapture

// Factory opens an audio capture stream.
type Factory func(Config) (Stream, error)

// Config controls audio capture stream selection.
type Config struct {
	// InputDevice selects the capture device, or empty for the system default.
	InputDevice string
	// OnPCM receives captured signed 16-bit mono PCM chunks at the package sample rate.
	// The slice is only valid until OnPCM returns; copy it before retaining it.
	OnPCM func([]byte)
}

// Stream is an opened audio capture stream.
type Stream interface {
	// Start begins delivering PCM chunks.
	// Start must be idempotent while the stream is open; calling Start on an
	// already-started stream should return nil.
	Start() error
	// Stop pauses PCM delivery without closing the stream.
	// Stop must be idempotent while the stream is open; calling Stop on an
	// already-stopped stream should return nil.
	Stop() error
	// Close releases the stream and its backend resources. Close may be called
	// without calling Stop first. A stream must not be used after Close returns.
	Close() error
}
