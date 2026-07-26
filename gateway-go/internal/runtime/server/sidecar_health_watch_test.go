package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A sidecar that answers ANYTHING is alive; only a transport failure is down.
// This is the distinction that matters operationally: the ASR sidecar returns
// 405 to a GET (it is POST-only) and that must not read as an outage, while a
// crash-looping container refuses the connection and must.
func TestLivenessCheckTreatsAnyHTTPResponseAsAlive(t *testing.T) {
	for _, code := range []int{200, 401, 404, 405, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		if err := livenessCheck(srv.URL)(); err != nil {
			t.Errorf("HTTP %d should read as alive, got %v", code, err)
		}
		srv.Close()
	}
}

func TestLivenessCheckReportsUnreachable(t *testing.T) {
	// Closed immediately: the port is dead, which is exactly the state the ASR
	// container spent 18,325 restarts in without anyone being told.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if err := livenessCheck(url)(); err == nil {
		t.Fatal("a refused connection must report an error")
	}
}
