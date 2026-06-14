// shaper.go — per-client response shaping. client.go answers "who is calling";
// this answers "does that caller want the output a little different, and how".
// It turns the streamResponse seam into a real extension point: a responseShaper
// adapts the upstream response (headers + body stream) for one client. Every
// client gets the zero-overhead identity shaper today; adding a real adaptation
// is a single case in shaperFor + a shaper type — the streaming/flush/header
// plumbing in streamResponse never changes.
package main

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
)

// responseShaper adapts an upstream response to what a particular client expects.
// identityShaper (the default for every client) is a pure pass-through with no
// allocation or copy. A transforming shaper wraps the body reader and/or edits
// headers.
type responseShaper interface {
	// header may adjust or inject response headers before they are written.
	header(h http.Header)
	// body returns the reader streamResponse copies to the client. identity
	// returns r unchanged (zero overhead); a transforming shaper wraps it.
	body(r io.Reader) io.Reader
}

// shaperFor picks the response shaper for a caller. Today every client gets the
// faithful pass-through (identityShaper). To make wormhole emit a client-specific
// shape, add a case here returning a shaper — e.g.:
//
//	case clientSomeApp:
//	    return newSSEDataShaper(func(data []byte) []byte { ...rewrite each event... })
//
// It is a var so a test can substitute a transforming shaper to exercise the
// shaping path; production code only ever reads it.
var shaperFor = func(client clientInfo) responseShaper {
	switch client.kind {
	default:
		return identityShaper{}
	}
}

// identityShaper passes the response through untouched (the hot-path default).
type identityShaper struct{}

func (identityShaper) header(http.Header)         {}
func (identityShaper) body(r io.Reader) io.Reader { return r }

// sseDataShaper is a reusable base for shapers that rewrite individual SSE
// `data:` payloads — the common case when a client needs streamed events tweaked
// — without re-implementing SSE framing. transform receives the bytes after
// `data:` (whitespace-trimmed) and returns the replacement; every other line
// (event:, id:, comments, the blank event separators) passes through. The work
// runs on a goroutine writing into a pipe, so it stays streaming (no buffering of
// the whole response). Only used when shaperFor returns one — the default path
// never allocates a pipe.
type sseDataShaper struct {
	transform func(data []byte) []byte
}

func newSSEDataShaper(fn func(data []byte) []byte) sseDataShaper {
	return sseDataShaper{transform: fn}
}

func (sseDataShaper) header(http.Header) {}

func (s sseDataShaper) body(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // SSE event lines can be large
		var werr error
		for werr == nil && sc.Scan() {
			line := sc.Bytes()
			if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				out := s.transform(bytes.TrimPrefix(data, []byte(" ")))
				_, werr = pw.Write(append(append([]byte("data: "), out...), '\n'))
			} else {
				_, werr = pw.Write(append(append([]byte(nil), line...), '\n'))
			}
		}
		if werr == nil {
			werr = sc.Err()
		}
		_ = pw.CloseWithError(werr) // nil err = clean EOF
	}()
	return pr
}
