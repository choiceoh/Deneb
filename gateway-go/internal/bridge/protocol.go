// Package bridge provides IPC frame encoding for the Plugin Host.
//
// Frames are encoded as newline-delimited JSON (NDJSON): each frame is a
// single line of JSON terminated by '\n'. This matches the WebSocket gateway
// protocol but uses a stream-oriented transport (Unix domain socket).
package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	// writerBufSize is the bufio.Writer buffer for IPC writes.
	// 32 KB is large enough to batch typical NDJSON frames.
	writerBufSize = 32 * 1024
)

// FrameWriter writes newline-delimited JSON frames with buffering.
// Each write is buffered and explicitly flushed to batch small frames
// into fewer syscalls on the Unix socket.
type FrameWriter struct {
	bw *bufio.Writer
}

// NewFrameWriter creates a new buffered frame writer.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{bw: bufio.NewWriterSize(w, writerBufSize)}
}

// WriteFrame writes any frame value as a newline-delimited JSON line
// and flushes the buffer to ensure the frame is sent immediately.
func (fw *FrameWriter) WriteFrame(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	b = append(b, '\n')
	if _, err := fw.bw.Write(b); err != nil {
		return err
	}
	return fw.bw.Flush()
}

// WriteRequest writes a RequestFrame as NDJSON.
func (fw *FrameWriter) WriteRequest(req *protocol.RequestFrame) error {
	return fw.WriteFrame(req)
}

// WriteResponse writes a ResponseFrame as NDJSON.
func (fw *FrameWriter) WriteResponse(resp *protocol.ResponseFrame) error {
	return fw.WriteFrame(resp)
}

// FrameReader reads newline-delimited JSON frames.
type FrameReader struct {
	scanner *bufio.Scanner
}

// NewFrameReader creates a new frame reader.
func NewFrameReader(r io.Reader) *FrameReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, protocol.MaxPayloadBytes), protocol.MaxPayloadBytes)
	return &FrameReader{scanner: scanner}
}

// ReadFrame reads the next JSON frame and returns its type and raw bytes.
func (fr *FrameReader) ReadFrame() (protocol.FrameType, []byte, error) {
	if !fr.scanner.Scan() {
		if err := fr.scanner.Err(); err != nil {
			return "", nil, fmt.Errorf("read frame: %w", err)
		}
		return "", nil, io.EOF
	}
	data := fr.scanner.Bytes()
	if len(data) == 0 {
		return "", nil, fmt.Errorf("empty frame")
	}
	buf := make([]byte, len(data))
	copy(buf, data)

	frameType, err := protocol.ParseFrameType(buf)
	if err != nil {
		return "", buf, err
	}
	return frameType, buf, nil
}
