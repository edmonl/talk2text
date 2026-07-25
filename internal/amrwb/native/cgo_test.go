package native

import "testing"

func TestDecoderClose(t *testing.T) {
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	decoder.Close()
	decoder.Close()

	if _, err := decoder.Decode([]byte{0x7c}, false); err == nil {
		t.Fatal("Decode() after Close() succeeded")
	}
}

func TestDecoderRejectsInvalidFrameSize(t *testing.T) {
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	size, _ := FrameSize(0x04)
	tests := [][]byte{
		nil,
		{0x04},
		make([]byte, size+1),
		{byte(10<<3) | 0x04},
	}
	for _, frame := range tests {
		if _, err := decoder.Decode(frame, false); err == nil {
			t.Fatalf("Decode() accepted a %d-byte frame", len(frame))
		}
	}
}

func TestDecoderAcceptsNoDataFrame(t *testing.T) {
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	if _, err := decoder.Decode([]byte{0x7c}, false); err != nil {
		t.Fatal(err)
	}
}

func TestFrameSize(t *testing.T) {
	want := [...]int{18, 24, 33, 37, 41, 47, 51, 59, 61, 6, 0, 0, 0, 0, 1, 1}
	for frameType, wantSize := range want {
		got, valid := FrameSize(byte(frameType << 3))
		if valid != (wantSize != 0) || got != wantSize {
			t.Errorf("FrameSize(frame type %d) = %d, %t; want %d, %t", frameType, got, valid, wantSize, wantSize != 0)
		}
	}
}
