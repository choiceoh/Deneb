package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"gzip":                true,
		"gzip, deflate, br":   true,
		"deflate, gzip;q=1.0": true,
		"deflate":             false,
		"":                    false,
		"x-gzip":              false, // must match the gzip token exactly, not a substring
	}
	for header, want := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		if header != "" {
			r.Header.Set("Accept-Encoding", header)
		}
		if got := clientAcceptsGzip(r); got != want {
			t.Errorf("clientAcceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestWriteRPCJSON(t *testing.T) {
	big := map[string]string{"x": strings.Repeat("탑솔라 거래 ", 500)} // well over gzipMinBytes

	// Client accepts gzip + large body → compressed, and decompresses back.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	writeRPCJSON(w, r, big, nil, "test")
	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("expected gzip, got %q", enc)
	}
	gr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	dec, _ := io.ReadAll(gr)
	if !bytes.Contains(dec, []byte("탑솔라")) {
		t.Fatal("decompressed body missing payload")
	}

	// No Accept-Encoding → plain body (never an undecodable mismatch).
	r2 := httptest.NewRequest(http.MethodPost, "/", nil)
	w2 := httptest.NewRecorder()
	writeRPCJSON(w2, r2, big, nil, "test")
	if w2.Header().Get("Content-Encoding") != "" {
		t.Fatal("must not gzip when the client did not advertise it")
	}
	if !bytes.Contains(w2.Body.Bytes(), []byte("탑솔라")) {
		t.Fatal("plain body missing payload")
	}

	// Small body → not gzipped even when accepted (framing overhead not worth it).
	r3 := httptest.NewRequest(http.MethodPost, "/", nil)
	r3.Header.Set("Accept-Encoding", "gzip")
	w3 := httptest.NewRecorder()
	writeRPCJSON(w3, r3, map[string]string{"ok": "1"}, nil, "test")
	if w3.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("small body should not be gzipped")
	}
}
