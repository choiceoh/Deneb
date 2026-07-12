package httputil

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUATransportInjectsUserAgentWithFakeRoundTripper(t *testing.T) {
	originalVersion := version
	SetVersion("test-build")
	t.Cleanup(func() { SetVersion(originalVersion) })

	var received string
	transport := &uaTransport{base: fakeRoundTripper(func(req *http.Request) (*http.Response, error) {
		received = req.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/resource", nil)

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("RoundTrip() status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if received != "Deneb-Gateway/test-build" {
		t.Fatalf("received User-Agent = %q, want configured Deneb agent", received)
	}
}

func TestUATransportPreservesExplicitHeaderAtProtocolBoundary(t *testing.T) {
	const customAgent = "caller-agent/1.0"
	transport := &uaTransport{base: fakeRoundTripper(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != customAgent {
			t.Fatalf("forwarded User-Agent = %q, want %q", got, customAgent)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/resource", nil)
	req.Header.Set("User-Agent", customAgent)

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
}

func TestUATransportPropagatesFakeRoundTripError(t *testing.T) {
	wantErr := errors.New("transport unavailable")
	transport := &uaTransport{base: fakeRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	})}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/resource", nil)

	resp, err := transport.RoundTrip(req)

	if resp != nil {
		t.Fatalf("RoundTrip() response = %#v, want nil", resp)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
}
