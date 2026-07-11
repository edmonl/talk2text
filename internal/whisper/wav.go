package whisper

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Fixed WAV/PCM parameters for this package: mono, 16-bit signed
// little-endian samples at 16 kHz.
const (
	riffChunkID       = "RIFF"
	waveFormat        = "WAVE"
	fmtSubchunkID     = "fmt "
	dataSubchunkID    = "data"
	riffChunkSizeBase = 36
	pcmAudioFormat    = 1
	fmtSubchunkSize   = 16
	numChannels       = 1
	bitsPerSample     = 16
	bytesPerSample    = bitsPerSample / 8
	// SampleRateHz is the fixed PCM sample rate expected by Whisper.
	SampleRateHz = 16000
)

// wavHeader mirrors the 44-byte canonical WAV/RIFF header layout exactly,
// field for field, so it can be written in a single binary.Write call.
// Reference: http://soundfile.sapp.org/doc/WaveFormat/
type wavHeader struct {
	ChunkID       [4]byte // "RIFF"
	ChunkSize     uint32  // file size - 8 (i.e. everything after this field)
	Format        [4]byte // "WAVE"
	Subchunk1ID   [4]byte // "fmt "
	Subchunk1Size uint32  // 16 for PCM
	AudioFormat   uint16  // 1 = PCM (uncompressed)
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32 // SampleRate * NumChannels * BitsPerSample/8
	BlockAlign    uint16 // NumChannels * BitsPerSample/8
	BitsPerSample uint16
	Subchunk2ID   [4]byte // "data"
	Subchunk2Size uint32  // size of the PCM payload that follows
}

func writeWavHeader(w io.Writer, pcmLen int) error {
	if pcmLen > math.MaxUint32-riffChunkSizeBase {
		return fmt.Errorf("pcm payload too large to encode")
	}
	dataLen := uint32(pcmLen)

	return binary.Write(w, binary.LittleEndian, &wavHeader{
		ChunkID:       [4]byte([]byte(riffChunkID)),
		ChunkSize:     riffChunkSizeBase + dataLen,
		Format:        [4]byte([]byte(waveFormat)),
		Subchunk1ID:   [4]byte([]byte(fmtSubchunkID)),
		Subchunk1Size: fmtSubchunkSize,
		AudioFormat:   pcmAudioFormat,
		NumChannels:   numChannels,
		SampleRate:    SampleRateHz,
		ByteRate:      SampleRateHz * numChannels * bytesPerSample,
		BlockAlign:    numChannels * bytesPerSample,
		BitsPerSample: bitsPerSample,
		Subchunk2ID:   [4]byte([]byte(dataSubchunkID)),
		Subchunk2Size: dataLen,
	})
}
