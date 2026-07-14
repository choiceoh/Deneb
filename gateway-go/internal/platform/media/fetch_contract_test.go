package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingBody struct {
	err error
}

func (b *failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failingBody) Close() error             { return nil }

func mediaResponse(req *http.Request, status int, header http.Header, body io.ReadCloser) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	if body == nil {
		body = http.NoBody
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       body,
		Request:    req,
	}
}

func TestFetchSuccessContract(t *testing.T) {
	payload := []byte("document-body")
	var seen atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen.Add(1)
		if req.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", req.Method)
		}
		if req.URL.String() != "https://media.example.test/download?id=42" {
			t.Errorf("url = %s", req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization = %q", got)
		}
		if got := req.Header.Get("X-Trace-ID"); got != "trace-1" {
			t.Errorf("trace header = %q", got)
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/pdf")
		header.Set("Content-Disposition", `attachment; filename="../reports/quarter.pdf"`)
		resp := mediaResponse(req, http.StatusOK, header, io.NopCloser(bytes.NewReader(payload)))
		resp.ContentLength = int64(len(payload))
		return resp, nil
	})}

	got, err := Fetch(context.Background(), FetchOptions{
		URL:      "https://media.example.test/download?id=42",
		MaxBytes: 1024,
		Headers: map[string]string{
			"Authorization": "Bearer token",
			"X-Trace-ID":    "trace-1",
		},
		Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Load() != 1 {
		t.Fatalf("requests = %d, want 1", seen.Load())
	}
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("data = %q", got.Data)
	}
	if got.ContentType != "application/pdf" {
		t.Fatalf("content type = %q", got.ContentType)
	}
	if got.FileName != "quarter.pdf" {
		t.Fatalf("file name = %q, want basename", got.FileName)
	}
	if got.Size != len(payload) {
		t.Fatalf("size = %d, want %d", got.Size, len(payload))
	}
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", got.StatusCode)
	}
	if got.FinalURL != "https://media.example.test/download?id=42" {
		t.Fatalf("final url = %q", got.FinalURL)
	}
}

func TestFetchReturnsBodyForEvery2xxStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "ok", status: http.StatusOK, body: "ok"},
		{name: "created", status: http.StatusCreated, body: "created"},
		{name: "accepted", status: http.StatusAccepted, body: "accepted"},
		{name: "partial content", status: http.StatusPartialContent, body: "partial"},
		{name: "no content", status: http.StatusNoContent, body: ""},
		{name: "last 2xx", status: 299, body: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return mediaResponse(req, tt.status, nil, io.NopCloser(strings.NewReader(tt.body))), nil
			})}
			got, err := Fetch(context.Background(), FetchOptions{
				URL:      "https://cdn.example.test/item",
				MaxBytes: 64,
				Client:   client,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.StatusCode != tt.status || string(got.Data) != tt.body {
				t.Fatalf("result = status%d body%q", got.StatusCode, got.Data)
			}
		})
	}
}

func TestFetchRejectsInvalidInitialURLsBeforeTransport(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return mediaResponse(req, http.StatusOK, nil, http.NoBody), nil
	})}
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "missing host", url: "https:///file"},
		{name: "file scheme", url: "file:///etc/passwd"},
		{name: "ftp scheme", url: "ftp://example.test/file"},
		{name: "loopback v4", url: "http://127.0.0.1/secret"},
		{name: "loopback v6", url: "http://[::1]/secret"},
		{name: "private v4", url: "http://192.168.1.10/secret"},
		{name: "metadata", url: "http://169.254.169.254/latest/meta-data"},
		{name: "azure wire server", url: "http://168.63.129.16/machine"},
		{name: "cgnat", url: "http://100.64.0.1/tailnet"},
		{name: "nat64 private", url: "http://[64:ff9b::c0a8:101]/secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), FetchOptions{URL: tt.url, Client: client})
			if err == nil {
				t.Fatal("invalid URL succeeded")
			}
			var fetchErr *MediaFetchError
			if !errors.As(err, &fetchErr) {
				t.Fatalf("error type = %T, want MediaFetchError", err)
			}
			if fetchErr.Code != ErrFetchFailed {
				t.Fatalf("code = %s", fetchErr.Code)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("transport called %d times for rejected URLs", calls.Load())
	}
}

func TestFetchValidatesEveryRedirectWithCustomClient(t *testing.T) {
	t.Run("public redirect succeeds and final url is reported", func(t *testing.T) {
		var calls atomic.Int64
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			switch req.URL.Host {
			case "origin.example.test":
				header := make(http.Header)
				header.Set("Location", "https://cdn.example.test/final")
				return mediaResponse(req, http.StatusFound, header, http.NoBody), nil
			case "cdn.example.test":
				return mediaResponse(req, http.StatusOK, nil, io.NopCloser(strings.NewReader("final"))), nil
			default:
				return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
			}
		})}
		got, err := Fetch(context.Background(), FetchOptions{
			URL:          "https://origin.example.test/start",
			MaxBytes:     32,
			MaxRedirects: 3,
			Client:       client,
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 {
			t.Fatalf("calls = %d, want 2", calls.Load())
		}
		if got.FinalURL != "https://cdn.example.test/final" {
			t.Fatalf("final url = %q", got.FinalURL)
		}
	})

	blockedTargets := []struct {
		name     string
		location string
	}{
		{name: "loopback", location: "http://127.0.0.1/admin"},
		{name: "private lan", location: "http://10.1.2.3/admin"},
		{name: "metadata", location: "http://169.254.169.254/latest/meta-data"},
		{name: "azure metadata", location: "http://168.63.129.16/machine"},
		{name: "unsupported scheme", location: "file:///etc/passwd"},
	}
	for _, tt := range blockedTargets {
		t.Run("blocked/"+tt.name, func(t *testing.T) {
			var calls atomic.Int64
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				header := make(http.Header)
				header.Set("Location", tt.location)
				return mediaResponse(req, http.StatusFound, header, http.NoBody), nil
			})}
			_, err := Fetch(context.Background(), FetchOptions{
				URL:          "https://origin.example.test/start",
				MaxRedirects: 5,
				Client:       client,
			})
			if err == nil {
				t.Fatal("unsafe redirect succeeded")
			}
			var fetchErr *MediaFetchError
			if !errors.As(err, &fetchErr) || fetchErr.Code != ErrFetchFailed {
				t.Fatalf("error = %#v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("unsafe destination reached transport, calls=%d", calls.Load())
			}
		})
	}
}

func TestFetchEnforcesRedirectLimitAndPreservesCallerPolicy(t *testing.T) {
	t.Run("max redirects is enforced", func(t *testing.T) {
		var calls atomic.Int64
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			step := calls.Load()
			header := make(http.Header)
			header.Set("Location", fmt.Sprintf("https://redirect.example.test/step-%d", step))
			return mediaResponse(req, http.StatusFound, header, http.NoBody), nil
		})}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:          "https://redirect.example.test/start",
			MaxRedirects: 2,
			Client:       client,
		})
		if err == nil || !strings.Contains(err.Error(), "too many redirects (2)") {
			t.Fatalf("error = %v", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("calls = %d, want 2", calls.Load())
		}
	})

	t.Run("caller redirect policy still runs after security policy", func(t *testing.T) {
		policyErr := errors.New("caller says stop")
		var policyCalls atomic.Int64
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Location", "https://cdn.example.test/final")
				return mediaResponse(req, http.StatusFound, header, http.NoBody), nil
			}),
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				policyCalls.Add(1)
				return policyErr
			},
		}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:    "https://origin.example.test/start",
			Client: client,
		})
		if err == nil || !errors.Is(err, policyErr) {
			t.Fatalf("error = %v", err)
		}
		if policyCalls.Load() != 1 {
			t.Fatalf("policy calls = %d", policyCalls.Load())
		}
	})

	t.Run("caller client is not mutated", func(t *testing.T) {
		original := func(_ *http.Request, _ []*http.Request) error { return nil }
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return mediaResponse(req, http.StatusOK, nil, http.NoBody), nil
			}),
			CheckRedirect: original,
		}
		before := reflect.ValueOf(client.CheckRedirect).Pointer()
		if _, err := Fetch(context.Background(), FetchOptions{
			URL:    "https://origin.example.test/start",
			Client: client,
		}); err != nil {
			t.Fatal(err)
		}
		after := reflect.ValueOf(client.CheckRedirect).Pointer()
		if before != after {
			t.Fatal("Fetch mutated caller client redirect function")
		}
	})
}

func TestFetchSizeLimitsContract(t *testing.T) {
	t.Run("content length rejects before body read", func(t *testing.T) {
		body := &countingReadCloser{Reader: strings.NewReader("0123456789")}
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := mediaResponse(req, http.StatusOK, nil, body)
			resp.ContentLength = 10
			return resp, nil
		})}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:      "https://cdn.example.test/large",
			MaxBytes: 5,
			Client:   client,
		})
		var fetchErr *MediaFetchError
		if !errors.As(err, &fetchErr) || fetchErr.Code != ErrMaxBytes {
			t.Fatalf("error = %#v", err)
		}
		if body.reads != 0 {
			t.Fatalf("body read %d times despite oversized content-length", body.reads)
		}
		if !body.closed {
			t.Fatal("body not closed")
		}
	})

	t.Run("unknown content length is bounded while reading", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := mediaResponse(req, http.StatusOK, nil, io.NopCloser(strings.NewReader("0123456789")))
			resp.ContentLength = -1
			return resp, nil
		})}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:      "https://cdn.example.test/stream",
			MaxBytes: 5,
			Client:   client,
		})
		var fetchErr *MediaFetchError
		if !errors.As(err, &fetchErr) || fetchErr.Code != ErrMaxBytes {
			t.Fatalf("error = %#v", err)
		}
		if !strings.Contains(fetchErr.Message, "response body exceeds limit 5 bytes") {
			t.Fatalf("message = %q", fetchErr.Message)
		}
	})

	t.Run("exact limit succeeds", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := mediaResponse(req, http.StatusOK, nil, io.NopCloser(strings.NewReader("12345")))
			resp.ContentLength = -1
			return resp, nil
		})}
		got, err := Fetch(context.Background(), FetchOptions{
			URL:      "https://cdn.example.test/exact",
			MaxBytes: 5,
			Client:   client,
		})
		if err != nil || string(got.Data) != "12345" {
			t.Fatalf("result = %+v/%v", got, err)
		}
	})
}

type countingReadCloser struct {
	io.Reader
	reads  int
	closed bool
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *countingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestFetchHTTPErrorContract(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "redirect without location",
			status: http.StatusFound,
			body:   " redirect response ",
			want:   "redirect response",
		},
		{
			name:   "bad request collapses whitespace",
			status: http.StatusBadRequest,
			body:   " first\n\n second\tthird ",
			want:   "first second third",
		},
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			body:   "token expired",
			want:   "token expired",
		},
		{
			name:   "not found",
			status: http.StatusNotFound,
			body:   "missing",
			want:   "missing",
		},
		{
			name:   "server error",
			status: http.StatusInternalServerError,
			body:   "upstream down",
			want:   "upstream down",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return mediaResponse(req, tt.status, nil, io.NopCloser(strings.NewReader(tt.body))), nil
			})}
			_, err := Fetch(context.Background(), FetchOptions{
				URL:    "https://cdn.example.test/item",
				Client: client,
			})
			var fetchErr *MediaFetchError
			if !errors.As(err, &fetchErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if fetchErr.Code != ErrHTTPError || fetchErr.Status != tt.status {
				t.Fatalf("error = %+v", fetchErr)
			}
			if !strings.Contains(fetchErr.Message, tt.want) {
				t.Fatalf("message = %q, want substring %q", fetchErr.Message, tt.want)
			}
		})
	}
}

func TestFetchTransportAndBodyErrors(t *testing.T) {
	t.Run("transport error preserves cause and redacts query", func(t *testing.T) {
		cause := errors.New("dial https://api.example.test/file?token=secret: refused")
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, cause
		})}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:    "https://api.example.test/file?token=secret",
			Client: client,
		})
		var fetchErr *MediaFetchError
		if !errors.As(err, &fetchErr) || fetchErr.Code != ErrFetchFailed {
			t.Fatalf("error = %#v", err)
		}
		if !errors.Is(err, cause) {
			t.Fatalf("cause was not unwrap-compatible: %v", err)
		}
		if strings.Contains(fetchErr.Message, "token=secret") {
			t.Fatalf("secret query leaked: %q", fetchErr.Message)
		}
		if !strings.Contains(fetchErr.Message, "?[REDACTED]") {
			t.Fatalf("redaction marker missing: %q", fetchErr.Message)
		}
	})

	t.Run("body read error preserves cause", func(t *testing.T) {
		cause := errors.New("connection reset")
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := mediaResponse(req, http.StatusOK, nil, &failingBody{err: cause})
			resp.ContentLength = -1
			return resp, nil
		})}
		_, err := Fetch(context.Background(), FetchOptions{
			URL:      "https://cdn.example.test/item",
			MaxBytes: 10,
			Client:   client,
		})
		var fetchErr *MediaFetchError
		if !errors.As(err, &fetchErr) || fetchErr.Code != ErrFetchFailed || !errors.Is(err, cause) {
			t.Fatalf("error = %#v", err)
		}
	})

	t.Run("canceled context is returned through fetch error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		})}
		_, err := Fetch(ctx, FetchOptions{
			URL:    "https://cdn.example.test/item",
			Client: client,
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

func TestMediaFetchErrorContract(t *testing.T) {
	cause := errors.New("root cause")
	err := &MediaFetchError{
		Code:    ErrHTTPError,
		Message: "HTTP 503",
		Status:  http.StatusServiceUnavailable,
		Cause:   cause,
	}
	if got := err.Error(); got != "media fetch error (http_error): HTTP 503" {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap did not expose cause")
	}
}

func TestParseContentDispositionSecurityAndEncoding(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "empty", header: "", want: ""},
		{name: "inline without name", header: "inline", want: ""},
		{name: "quoted", header: `attachment; filename="report.pdf"`, want: "report.pdf"},
		{name: "unix traversal basename", header: `attachment; filename="../../secret.txt"`, want: "secret.txt"},
		{name: "windows traversal basename", header: `attachment; filename="..\\..\\secret.txt"`, want: "secret.txt"},
		{name: "mixed separators", header: `attachment; filename="folder\\nested/file.csv"`, want: "file.csv"},
		{name: "utf8 extended filename", header: `attachment; filename*=UTF-8''%ED%95%9C%EA%B8%80.pdf`, want: "한글.pdf"},
		{name: "invalid media type", header: `attachment; filename="unterminated`, want: ""},
		{name: "empty filename", header: `attachment; filename=""`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseContentDispositionFileName(tt.header); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadBodySnippetContract(t *testing.T) {
	tests := []struct {
		name string
		body string
		max  int
		want string
	}{
		{name: "empty", body: "", max: 10, want: ""},
		{name: "short", body: "hello", max: 10, want: "hello"},
		{name: "exact", body: "hello", max: 5, want: "hello"},
		{name: "truncated", body: "hello world", max: 5, want: "hello"},
		{name: "whitespace collapsed", body: " a\n\tb  c ", max: 20, want: "a b c"},
		{name: "zero cap", body: "hello", max: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readBodySnippet(strings.NewReader(tt.body), tt.max); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactURLContract(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		raw  string
		want string
	}{
		{
			name: "query is replaced",
			msg:  "GET https://api.example.test/file?token=secret failed",
			raw:  "https://api.example.test/file?token=secret",
			want: "GET https://api.example.test/file?[REDACTED] failed",
		},
		{
			name: "all repeated raw urls replaced",
			msg:  "https://a.test/p?k=v then https://a.test/p?k=v",
			raw:  "https://a.test/p?k=v",
			want: "https://a.test/p?[REDACTED] then https://a.test/p?[REDACTED]",
		},
		{
			name: "no query unchanged",
			msg:  "https://a.test/p failed",
			raw:  "https://a.test/p",
			want: "https://a.test/p failed",
		},
		{
			name: "invalid raw unchanged",
			msg:  "original",
			raw:  "http://%zz",
			want: "original",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURL(tt.msg, tt.raw); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateURLAndPrivateIPBoundaryMatrix(t *testing.T) {
	urls := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "public domain http", raw: "http://example.test/a", wantErr: false},
		{name: "public domain https", raw: "https://example.test/a", wantErr: false},
		{name: "public ipv4", raw: "https://8.8.8.8/a", wantErr: false},
		{name: "public ipv6", raw: "https://[2606:4700:4700::1111]/a", wantErr: false},
		{name: "zero network start", raw: "http://0.0.0.0/a", wantErr: true},
		{name: "zero network end", raw: "http://0.255.255.255/a", wantErr: true},
		{name: "ten start", raw: "http://10.0.0.0/a", wantErr: true},
		{name: "ten end", raw: "http://10.255.255.255/a", wantErr: true},
		{name: "172 private start", raw: "http://172.16.0.0/a", wantErr: true},
		{name: "172 private end", raw: "http://172.31.255.255/a", wantErr: true},
		{name: "172 before private", raw: "http://172.15.255.255/a", wantErr: false},
		{name: "172 after private", raw: "http://172.32.0.0/a", wantErr: false},
		{name: "192 private start", raw: "http://192.168.0.0/a", wantErr: true},
		{name: "192 private end", raw: "http://192.168.255.255/a", wantErr: true},
		{name: "cgnat start", raw: "http://100.64.0.0/a", wantErr: true},
		{name: "cgnat end", raw: "http://100.127.255.255/a", wantErr: true},
		{name: "cgnat before", raw: "http://100.63.255.255/a", wantErr: false},
		{name: "cgnat after", raw: "http://100.128.0.0/a", wantErr: false},
		{name: "benchmark start", raw: "http://198.18.0.0/a", wantErr: true},
		{name: "benchmark end", raw: "http://198.19.255.255/a", wantErr: true},
		{name: "multicast start", raw: "http://224.0.0.0/a", wantErr: true},
		{name: "reserved end", raw: "http://255.255.255.255/a", wantErr: true},
		{name: "unique local ipv6", raw: "http://[fc00::1]/a", wantErr: true},
		{name: "link local ipv6", raw: "http://[fe80::1]/a", wantErr: true},
		{name: "ipv4 mapped private", raw: "http://[::ffff:10.0.0.1]/a", wantErr: true},
	}
	for _, tt := range urls {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestEmbeddedIPv4TransitionContract(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want []string
	}{
		{name: "plain ipv4", ip: "192.168.1.1", want: []string{}},
		{name: "plain ipv6", ip: "2606:4700::1111", want: []string{}},
		{name: "6to4", ip: "2002:c0a8:0101::", want: []string{"192.168.1.1"}},
		{name: "nat64", ip: "64:ff9b::0a00:0001", want: []string{"10.0.0.1"}},
		{name: "teredo", ip: "2001:0000::3fff:fefe", want: []string{"192.0.1.1"}},
		{name: "isatap zero", ip: "2001:db8::0200:5efe:c0a8:0101", want: []string{"192.168.1.1"}},
		{name: "isatap two", ip: "2001:db8::0000:5efe:0a00:0001", want: []string{"10.0.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips := embeddedIPv4s(net.ParseIP(tt.ip))
			got := make([]string, len(ips))
			for i, ip := range ips {
				got[i] = ip.String()
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSSRFSafeDialerRejectsMalformedAndLocalAddress(t *testing.T) {
	dial := SSRFSafeDialer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := dial(ctx, "tcp", "missing-port"); err == nil {
		t.Fatal("malformed address succeeded")
	}
	if _, err := dial(ctx, "tcp", "127.0.0.1:80"); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("loopback dial error = %v", err)
	}
}

func TestLoggerContract(t *testing.T) {
	if Logger(nil) != slog.Default() {
		t.Fatal("nil base did not return slog.Default")
	}
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	got := Logger(base)
	if got == nil {
		t.Fatal("derived logger is nil")
	}
	if got == base {
		t.Fatal("non-nil base should return package-attributed child")
	}
}

func TestMediaFetchErrorJSONOmitsCauseAndData(t *testing.T) {
	err := &MediaFetchError{Code: ErrFetchFailed, Message: "failed", Cause: errors.New("secret cause")}
	raw, marshalErr := jsonMarshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "secret cause") || strings.Contains(string(raw), "Cause") {
		t.Fatalf("cause leaked into JSON: %s", raw)
	}
	result := FetchResult{Data: []byte("secret bytes"), ContentType: "text/plain", Size: 12}
	raw, marshalErr = jsonMarshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "secret bytes") || strings.Contains(string(raw), "Data") {
		t.Fatalf("data leaked into JSON: %s", raw)
	}
}

func jsonMarshal(v any) ([]byte, error) {
	// Small wrapper keeps this test file focused on the public JSON contract and
	// gives static analysis a single obvious call site.
	return json.Marshal(v)
}

func TestURLParserAssumptionForRedaction(t *testing.T) {
	raw := "https://user:pass@example.test/path?token=secret#fragment"
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "example.test" || u.User.String() != "user:pass" || u.Path != "/path" || u.RawQuery != "token=secret" {
		t.Fatalf("parsed URL = %+v", u)
	}
	redacted := redactURL("request "+raw+" failed", raw)
	if strings.Contains(redacted, "token=secret") {
		t.Fatalf("query leaked: %s", redacted)
	}
}
