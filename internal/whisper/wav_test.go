package whisper

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestWriteWavHeader(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}
	var header bytes.Buffer
	if err := writeWavHeader(&header, len(pcm)); err != nil {
		t.Fatal(err)
	}
	wav := append(header.Bytes(), pcm...)
	if !bytes.Equal(wav[:4], []byte("RIFF")) {
		t.Fatalf("missing RIFF header: %q", wav[:4])
	}
	if !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Fatalf("missing WAVE header: %q", wav[8:12])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != SampleRateHz {
		t.Fatalf("sample rate = %d, want %d", got, SampleRateHz)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("data length = %d, want %d", got, len(pcm))
	}
	if !bytes.Equal(wav[44:], pcm) {
		t.Fatalf("payload mismatch")
	}
}

func TestWriteWavHeaderRejectsOversizedPCM(t *testing.T) {
	err := writeWavHeader(&bytes.Buffer{}, math.MaxUint32-35)
	if err == nil {
		t.Fatal("writeWavHeader succeeded")
	}
	if !strings.Contains(err.Error(), "pcm payload too large to encode") {
		t.Fatalf("err = %v", err)
	}
}
