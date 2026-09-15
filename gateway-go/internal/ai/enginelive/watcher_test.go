package enginelive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingSink captures every SetEngineDown call in order.
type recordingSink struct {
	mu    sync.Mutex
	calls []string // "endpoint=model,model" ("" models = up)
	last  map[string][]string
}

func (s *recordingSink) SetEngineDown(endpoint string, models []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, endpoint+"="+strings.Join(models, ","))
	if s.last == nil {
		s.last = make(map[string][]string)
	}
	s.last[endpoint] = append([]string(nil), models...)
}

func (s *recordingSink) down(endpoint string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last[endpoint]
}

const ep = "http://10.0.0.5:8000/metrics"

func newTestWatcher(t *testing.T, models func(string) []string) (*Watcher, *recordingSink, *bytes.Buffer) {
	t.Helper()
	sink := &recordingSink{}
	var logs bytes.Buffer
	w := New([]string{ep}, models, sink, slog.New(slog.NewTextHandler(&logs, nil)))
	if w == nil {
		t.Fatal("New returned nil for a private endpoint")
	}
	return w, sink, &logs
}

func TestObserve_RefusalMarksDownAtOnce(t *testing.T) {
	w, sink, logs := newTestWatcher(t, func(string) []string { return []string{"glm-flash-low", "glm-flash", "glm-flash"} })
	st := w.engines[ep]

	w.observe(ep, st, verdictRefusing, "health 503 draining (handing over to campaign)")

	if !st.down {
		t.Fatal("a refusal from the engine itself must mark it down on the first probe")
	}
	if got := strings.Join(sink.down(ep), ","); got != "glm-flash,glm-flash-low" {
		t.Fatalf("sink models = %q, want sorted and de-duplicated", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "handing over to campaign") {
		t.Fatalf("the down transition must be a single WARN carrying the engine's reason:\n%s", logs.String())
	}
}

func TestObserve_SilenceMustRepeatBeforeDown(t *testing.T) {
	w, sink, _ := newTestWatcher(t, func(string) []string { return []string{"m"} })
	st := w.engines[ep]

	w.observe(ep, st, verdictSilent, "timeout")
	if st.down || len(sink.calls) != 0 {
		t.Fatal("one silent probe marked the engine down; a lost packet over Wi-Fi is not an outage")
	}
	// A ready answer in between resets the count.
	w.observe(ep, st, verdictReady, "")
	w.observe(ep, st, verdictSilent, "timeout")
	if st.down {
		t.Fatal("non-consecutive silences marked the engine down")
	}
	w.observe(ep, st, verdictSilent, "timeout")
	if !st.down {
		t.Fatalf("%d consecutive silent probes did not mark the engine down", silentToDown)
	}
}

func TestObserve_RecoveryNeedsTwoReadyAnswers(t *testing.T) {
	w, sink, logs := newTestWatcher(t, func(string) []string { return []string{"m"} })
	st := w.engines[ep]
	start := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return start }
	w.observe(ep, st, verdictRefusing, "connection refused")

	w.now = func() time.Time { return start.Add(90 * time.Second) }
	w.observe(ep, st, verdictReady, "")
	if !st.down {
		t.Fatal("one ready answer brought the engine back; a flapping boot would pull turns onto it")
	}
	// A refusal between the two ready answers restarts the count.
	w.observe(ep, st, verdictRefusing, "connection refused")
	w.observe(ep, st, verdictReady, "")
	if !st.down {
		t.Fatal("ready count survived an intervening refusal")
	}
	w.observe(ep, st, verdictReady, "")
	if st.down {
		t.Fatalf("%d consecutive ready answers did not bring the engine back", readyToUp)
	}
	if got := sink.calls[len(sink.calls)-1]; got != ep+"=" {
		t.Fatalf("last sink call = %q, want the empty set (engine serving)", got)
	}
	if !strings.Contains(logs.String(), "accepting requests again") || !strings.Contains(logs.String(), "downFor=1m30s") {
		t.Fatalf("recovery log must say how long the engine was down:\n%s", logs.String())
	}
}

func TestObserve_ModelsReResolvedWhileDown(t *testing.T) {
	names := []string{"glm-flash-local"}
	w, sink, logs := newTestWatcher(t, func(string) []string { return names })
	st := w.engines[ep]
	w.observe(ep, st, verdictRefusing, "connection refused")

	// The router entry is renamed during the outage.
	names = []string{"glm-flash"}
	w.observe(ep, st, verdictRefusing, "connection refused")

	if got := strings.Join(sink.down(ep), ","); got != "glm-flash" {
		t.Fatalf("sink models = %q after the rename, want the new name", got)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("WARN lines = %d, want exactly one for the whole outage:\n%s", n, logs.String())
	}
}

func TestProbe_ClassifiesTheEngineAnswer(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		want       verdict
		wantReason string
	}{
		{"serving", http.StatusOK, `{"status":"ok"}`, verdictReady, ""},
		// An engine without /health still answered: alive.
		{"no health route", http.StatusNotFound, "not found", verdictReady, ""},
		{
			"draining for a handover", http.StatusServiceUnavailable,
			`{"status":"draining","handing_over_to":"kernel-campaign","running":1,"waiting":0}`,
			verdictRefusing, "health 503 draining (handing over to kernel-campaign)",
		},
		{"stopping", http.StatusServiceUnavailable, `{"status":"stopping"}`, verdictRefusing, "health 503 stopping"},
		{"opaque 5xx", http.StatusBadGateway, "<html>", verdictRefusing, "health 502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					t.Errorf("probe path = %q, want /health", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			w := &Watcher{client: server.Client()}

			got, reason := w.probe(context.Background(), server.URL+"/health")
			if got != tc.want || reason != tc.wantReason {
				t.Fatalf("probe = (%v, %q), want (%v, %q)", got, reason, tc.want, tc.wantReason)
			}
		})
	}
}

func TestProbe_RefusedConnectionIsRefusing(t *testing.T) {
	// A port nothing listens on: the host answers with a reset, which is as
	// definite as a 503.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	w := &Watcher{client: &http.Client{Timeout: time.Second}}
	got, reason := w.probe(context.Background(), "http://"+addr+"/health")
	if got != verdictRefusing || reason != "connection refused" {
		t.Fatalf("probe = (%v, %q), want (refusing, connection refused)", got, reason)
	}
}

func TestProbe_NoAnswerIsSilent(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	w := &Watcher{client: &http.Client{Timeout: 50 * time.Millisecond}}
	if got, _ := w.probe(context.Background(), server.URL+"/health"); got != verdictSilent {
		t.Fatalf("probe = %v, want silent for a server that never answers", got)
	}
}

func TestNew_ProbesOnlyPrivateHostsAtTheirHealthRoute(t *testing.T) {
	sink := &recordingSink{}
	models := func(string) []string { return nil }
	w := New([]string{
		"http://10.0.0.5:8000/metrics",
		" http://10.0.0.5:8000/metrics ", // duplicate
		"https://api.example.com/metrics",
		"http://engine.internal:8000/metrics", // names are never resolved
		"",
	}, models, sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w == nil {
		t.Fatal("New returned nil with one valid endpoint")
	}
	if got := strings.Join(w.endpoints, ","); got != "http://10.0.0.5:8000/metrics" {
		t.Fatalf("endpoints = %q, want only the private one, once", got)
	}
	if got := w.engines["http://10.0.0.5:8000/metrics"].healthURL; got != "http://10.0.0.5:8000/health" {
		t.Fatalf("healthURL = %q", got)
	}

	if New([]string{"https://api.example.com/metrics"}, models, sink, nil) != nil {
		t.Error("New watched a public endpoint")
	}
	if New(nil, models, sink, nil) != nil {
		t.Error("New with no endpoints returned a watcher")
	}
	if New([]string{ep}, models, nil, nil) != nil {
		t.Error("New without a sink returned a watcher")
	}
	var nilWatcher *Watcher
	nilWatcher.Start(context.Background()) // must not panic
}

func TestRun_ProbesImmediatelyAndStopsWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"status":"stopping"}`)
	}))
	defer server.Close()

	reported := make(chan struct{}, 1)
	sink := &notifyingSink{reported: reported}
	// httptest listens on loopback, which is a private host.
	endpoint := server.URL + "/metrics"
	w := New([]string{endpoint}, func(string) []string { return []string{"m"} }, sink,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w == nil {
		t.Fatal("New returned nil for a loopback endpoint")
	}
	w.interval = time.Hour // only the immediate probe may run in this test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	// Wait for the probe's verdict to reach the sink, not for the request to
	// reach the server: cancelling between the two would end the probe with a
	// context error and prove nothing.
	select {
	case <-reported:
	case <-time.After(2 * time.Second):
		t.Fatal("no probe before the first tick: a restart mid-outage would keep retrying for a whole interval")
	}
	if got := fmt.Sprint(sink.down(endpoint)); got != "[m]" {
		t.Fatalf("sink = %s after a refusing probe, want [m]", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after its context ended")
	}
}

// notifyingSink is a recordingSink that signals each report.
type notifyingSink struct {
	recordingSink
	reported chan struct{}
}

func (s *notifyingSink) SetEngineDown(endpoint string, models []string) {
	s.recordingSink.SetEngineDown(endpoint, models)
	select {
	case s.reported <- struct{}{}:
	default:
	}
}
