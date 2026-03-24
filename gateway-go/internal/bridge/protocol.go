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

// FrameWriter writes newline-delimited JSON frames.
type FrameWriter struct {
	w io.Writer
}

// NewFrameWriter creates a new frame writer.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w}
}

// WriteFrame writes any frame value as a newline-delimited JSON line.
func (fw *FrameWriter) WriteFrame(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	b = append(b, '\n')
	_, err = fw.w.Write(b)
	return err
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
