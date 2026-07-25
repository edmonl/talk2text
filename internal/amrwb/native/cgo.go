// Package native exposes the decoder interface from the vendored OpenCORE
// AMR-WB sources.
package native

/*
#cgo CFLAGS: -O3 -std=c99
#cgo LDFLAGS: -lm

#include "dec_if.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

const (
	// SamplesPerFrame is the number of decoded PCM samples in one AMR-WB
	// frame.
	SamplesPerFrame = 320
)

var frameBytes = [...]int{18, 24, 33, 37, 41, 47, 51, 59, 61, 6, 0, 0, 0, 0, 1, 1}

// FrameSize returns the encoded size of an AMR-WB storage frame, including
// its one-byte header. The boolean is false for reserved frame types.
func FrameSize(header byte) (int, bool) {
	size := frameBytes[int(header>>3)&0x0f]
	return size, size != 0
}

// Decoder holds the state required to decode a sequence of AMR-WB frames.
// A Decoder must not be used concurrently.
type Decoder struct {
	state unsafe.Pointer
}

// NewDecoder initializes an AMR-WB decoder.
func NewDecoder() (*Decoder, error) {
	state := C.D_IF_init()
	if state == nil {
		return nil, errors.New("initialize AMR-WB decoder")
	}
	return &Decoder{state: state}, nil
}

// Decode decodes one complete AMR-WB storage frame.
func (d *Decoder) Decode(frame []byte, badFrame bool) ([SamplesPerFrame]int16, error) {
	var pcm [SamplesPerFrame]int16
	if d.state == nil {
		return pcm, errors.New("AMR-WB decoder is closed")
	}
	if len(frame) == 0 {
		return pcm, errors.New("invalid AMR-WB frame size")
	}
	if want, valid := FrameSize(frame[0]); !valid || len(frame) != want {
		return pcm, errors.New("invalid AMR-WB frame size")
	}

	bfi := C.int(0)
	if badFrame {
		bfi = 1
	}
	C.D_IF_decode(
		d.state,
		(*C.uchar)(unsafe.Pointer(&frame[0])),
		(*C.short)(unsafe.Pointer(&pcm[0])),
		bfi,
	)
	return pcm, nil
}

// Close releases the decoder state. It is safe to call Close more than once.
func (d *Decoder) Close() {
	if d.state == nil {
		return
	}
	C.D_IF_exit(d.state)
	d.state = nil
}
