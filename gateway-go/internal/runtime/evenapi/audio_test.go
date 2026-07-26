package evenapi

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The ASR sidecar uses the mime type only to pick a filename and otherwise
// takes the bytes as given, so raw PCM handed over as "audio/wav" decodes to
// nothing. The header is the only part of the glasses audio path that can be
// checked without a microphone, so it is checked exactly.
func TestPcmToWavHeaderIsWellFormed(t *testing.T) {
	pcm := make([]byte, 320) // 10ms at 16 kHz, 16-bit mono
	for i := range pcm {
		pcm[i] = byte(i)
	}
	out := PcmToWav(pcm, PcmSampleRate)

	if got, want := len(out), 44+len(pcm); got != want {
		t.Fatalf("length = %d, want %d (44-byte header + payload)", got, want)
	}
	if !bytes.Equal(out[0:4], []byte("RIFF")) || !bytes.Equal(out[8:12], []byte("WAVE")) {
		t.Fatalf("not a RIFF/WAVE container: %q", out[:12])
	}
	if !bytes.Equal(out[12:16], []byte("fmt ")) || !bytes.Equal(out[36:40], []byte("data")) {
		t.Fatalf("chunk ids wrong: fmt=%q data=%q", out[12:16], out[36:40])
	}

	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(out[at : at+4]) }
	u16 := func(at int) uint16 { return binary.LittleEndian.Uint16(out[at : at+2]) }

	if got := u32(4); got != uint32(36+len(pcm)) {
		t.Errorf("RIFF size = %d, want %d", got, 36+len(pcm))
	}
	if got := u32(16); got != 16 {
		t.Errorf("fmt chunk size = %d, want 16", got)
	}
	if got := u16(20); got != 1 {
		t.Errorf("format = %d, want 1 (uncompressed PCM)", got)
	}
	if got := u16(22); got != 1 {
		t.Errorf("channels = %d, want 1", got)
	}
	if got := u32(24); got != PcmSampleRate {
		t.Errorf("sample rate = %d, want %d", got, PcmSampleRate)
	}
	// byteRate and blockAlign are what a decoder uses to find sample
	// boundaries; getting them wrong yields plausible-looking noise rather
	// than an error, which is the worst possible failure for a transcript.
	if got := u32(28); got != PcmSampleRate*2 {
		t.Errorf("byte rate = %d, want %d", got, PcmSampleRate*2)
	}
	if got := u16(32); got != 2 {
		t.Errorf("block align = %d, want 2", got)
	}
	if got := u16(34); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if got := u32(40); got != uint32(len(pcm)) {
		t.Errorf("data size = %d, want %d", got, len(pcm))
	}
	if !bytes.Equal(out[44:], pcm) {
		t.Error("payload was altered")
	}
}

func TestPcmToWavDefaultsSampleRate(t *testing.T) {
	// A client that omits sampleRate must still produce a decodable clip at the
	// rate the glasses actually use, not a zero-rate header.
	for _, rate := range []int{0, -1} {
		out := PcmToWav([]byte{1, 2}, rate)
		if got := binary.LittleEndian.Uint32(out[24:28]); got != PcmSampleRate {
			t.Errorf("sampleRate %d → header %d, want %d", rate, got, PcmSampleRate)
		}
	}
}

func TestPcmToWavHonoursAnotherRate(t *testing.T) {
	out := PcmToWav([]byte{1, 2, 3, 4}, 8000)
	if got := binary.LittleEndian.Uint32(out[24:28]); got != 8000 {
		t.Errorf("sample rate = %d, want 8000", got)
	}
	if got := binary.LittleEndian.Uint32(out[28:32]); got != 16000 {
		t.Errorf("byte rate = %d, want 16000 (8000 × 2)", got)
	}
}

func TestPcmToWavEmptyClipStaysValid(t *testing.T) {
	// The handler rejects empty audio before this point, but a header that
	// claims bytes it does not have would be a decoder crash rather than a
	// clean refusal.
	out := PcmToWav(nil, PcmSampleRate)
	if len(out) != 44 {
		t.Fatalf("length = %d, want 44", len(out))
	}
	if got := binary.LittleEndian.Uint32(out[40:44]); got != 0 {
		t.Errorf("data size = %d, want 0", got)
	}
}
