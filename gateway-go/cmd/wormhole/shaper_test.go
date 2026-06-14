package main

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// identityShaper is the default for every client today: a byte-exact pass-through.
func TestIdentityShaperPassThrough(t *testing.T) {
	s := identityShaper{}
	h := http.Header{}
	s.header(h)
	if len(h) != 0 {
		t.Errorf("identity header() mutated headers: %v", h)
	}
	in := "data: {\"a\":1}\n\ndata: [DONE]\n\n"
	out, err := io.ReadAll(s.body(strings.NewReader(in)))
	if err != nil {
		t.Fatalf("body read: %v", err)
	}
	if string(out) != in {
		t.Errorf("identity body altered stream:\n got %q\nwant %q", out, in)
	}
}

// shaperFor returns identity for every known client kind — the foundation ships
// with no client-specific shaping. This pins that invariant so a future shaper is
// added deliberately (and this test updated alongside it).
func TestShaperForDefaultsToIdentity(t *testing.T) {
	for _, k := range []clientKind{
		clientDeneb, clientClaudeCode, clientOpenAISDK,
		clientAnthropicSDK, clientCurl, clientUnknown,
	} {
		if _, ok := shaperFor(clientInfo{kind: k}).(identityShaper); !ok {
			t.Errorf("shaperFor(%q) = %T, want identityShaper", k, shaperFor(clientInfo{kind: k}))
		}
	}
}

// sseDataShaper rewrites each `data:` payload while leaving SSE framing (event:,
// comments, blank separators) untouched — and stays streaming via the pipe.
func TestSSEDataShaperRewritesDataLines(t *testing.T) {
	s := newSSEDataShaper(func(data []byte) []byte {
		if bytes.Equal(data, []byte("[DONE]")) {
			return data // sentinel passes through
		}
		return append([]byte("X"), data...)
	})
	in := "event: message\n" +
		"data: hello\n" +
		": keep-alive comment\n" +
		"data: world\n" +
		"\n" +
		"data: [DONE]\n"
	want := "event: message\n" +
		"data: Xhello\n" +
		": keep-alive comment\n" +
		"data: Xworld\n" +
		"\n" +
		"data: [DONE]\n"
	out, err := io.ReadAll(s.body(strings.NewReader(in)))
	if err != nil {
		t.Fatalf("body read: %v", err)
	}
	if string(out) != want {
		t.Errorf("sse shaper output:\n got %q\nwant %q", out, want)
	}
}

// streamResponse must route the body through shaperFor's shaper. Substitute a
// transforming shaper and prove the bytes the client receives are shaped — this
// also exercises the header() hook and the streamResponse copy loop.
func TestStreamResponseAppliesShaper(t *testing.T) {
	orig := shaperFor
	t.Cleanup(func() { shaperFor = orig })
	shaperFor = func(clientInfo) responseShaper {
		return testShaper{}
	}

	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: hi\n")),
	}
	rec := httptest.NewRecorder()
	streamResponse(clientInfo{kind: clientDeneb}, rec, upstream)

	res := rec.Result()
	if got := res.Header.Get("X-Shaped"); got != "1" {
		t.Errorf("shaper header() not applied: X-Shaped=%q", got)
	}
	body, _ := io.ReadAll(res.Body)
	if got := string(body); got != "data: SHAPED\n" {
		t.Errorf("shaper body() not applied: got %q", got)
	}
}

// testShaper is a streamResponse stand-in: it marks the header and rewrites every
// data line to a fixed token, proving the shaper sits on both the header and body
// paths.
type testShaper struct{}

func (testShaper) header(h http.Header) { h.Set("X-Shaped", "1") }
func (testShaper) body(r io.Reader) io.Reader {
	var b bytes.Buffer
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if bytes.HasPrefix(sc.Bytes(), []byte("data:")) {
			b.WriteString("data: SHAPED\n")
		} else {
			b.Write(append(sc.Bytes(), '\n'))
		}
	}
	return &b
}
