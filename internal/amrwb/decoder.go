// Package amrwb decodes single-channel AMR-WB storage-format audio into
// signed 16-bit, little-endian PCM at 16 kHz.
package amrwb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/edmonl/talk2text/internal/amrwb/native"
)

const (
	// Magic is the file signature for a single-channel AMR-WB storage stream.
	Magic = "#!AMR-WB\n"

	bytesPerSample   = 2
	pcmBytesPerFrame = native.SamplesPerFrame * bytesPerSample
)

var (
	// ErrInvalidHeader indicates that input is not a single-channel AMR-WB
	// storage stream.
	ErrInvalidHeader = errors.New("invalid AMR-WB header")
	// ErrInvalidFrame indicates that an encoded frame is reserved, truncated,
	// or otherwise invalid.
	ErrInvalidFrame = errors.New("invalid AMR-WB frame")
	// ErrTooLong indicates that the input contains more frames than permitted.
	ErrTooLong = errors.New("AMR-WB audio is too long")
)

// Decode converts a complete single-channel AMR-WB storage stream into
// signed 16-bit, little-endian PCM at 16 kHz.
func Decode(data []byte, maxFrames int) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(Magic)) {
		return nil, ErrInvalidHeader
	}

	frames := data[len(Magic):]
	frameCount, err := validateFrames(frames, maxFrames)
	if err != nil {
		return nil, err
	}
	if frameCount == 0 {
		return []byte{}, nil
	}
	if frameCount > math.MaxInt/pcmBytesPerFrame {
		return nil, fmt.Errorf("%w: decoded audio is too large", ErrTooLong)
	}

	pcm := make([]byte, 0, frameCount*pcmBytesPerFrame)
	decoder, err := native.NewDecoder()
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	for offset := 0; offset < len(frames); {
		header := frames[offset]
		size, _ := native.FrameSize(header)
		frame := frames[offset : offset+size]
		samples, err := decoder.Decode(frame, header&0x04 == 0)
		if err != nil {
			return nil, fmt.Errorf("%w at byte %d: %v", ErrInvalidFrame, len(Magic)+offset, err)
		}
		for _, sample := range samples {
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(sample))
		}
		offset += size
	}
	return pcm, nil
}

func validateFrames(frames []byte, maxFrames int) (int, error) {
	count := 0
	for offset := 0; offset < len(frames); {
		size, valid := native.FrameSize(frames[offset])
		if !valid {
			return 0, fmt.Errorf("%w at byte %d: reserved frame type", ErrInvalidFrame, len(Magic)+offset)
		}
		if size > len(frames)-offset {
			return 0, fmt.Errorf("%w at byte %d: need %d bytes, have %d", ErrInvalidFrame, len(Magic)+offset, size, len(frames)-offset)
		}
		count++
		if count > maxFrames {
			return 0, fmt.Errorf("%w: limit is %d frames", ErrTooLong, maxFrames)
		}
		offset += size
	}
	return count, nil
}
