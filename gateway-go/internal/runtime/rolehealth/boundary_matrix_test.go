package rolehealth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	rtevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
)

func roleHealthTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestIsLocalURLBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw string
		want      bool
	}{
		{
			name: "localhost http",
			raw:  "http://localhost:8000/v1",
			want: true,
		},
		{
			name: "localhost https",
			raw:  "https://localhost/v1",
			want: true,
		},
		{
			name: "localhost uppercase",
			raw:  "HTTP://LOCALHOST:8000",
			want: true,
		},
		{
			name: "localhost dot",
			raw:  "http://localhost.:8000",
			want: true,
		},
		{
			name: "ipv4 canonical",
			raw:  "http://127.0.0.1:8000",
			want: true,
		},
		{
			name: "ipv4 loopback two",
			raw:  "http://127.0.0.2:8000",
			want: true,
		},
		{
			name: "ipv4 loopback last",
			raw:  "http://127.255.255.255:8000",
			want: true,
		},
		{
			name: "ipv6 loopback",
			raw:  "http://[::1]:8000",
			want: true,
		},
		{
			name: "ipv6 expanded",
			raw:  "http://[0:0:0:0:0:0:0:1]:8000",
			want: true,
		},
		{
			name: "mapped loopback",
			raw:  "http://[::ffff:127.0.0.1]:8000",
			want: true,
		},
		{
			name: "private 10",
			raw:  "http://10.0.0.1:8000",
			want: false,
		},
		{
			name: "private 172",
			raw:  "http://172.16.0.1:8000",
			want: false,
		},
		{
			name: "private 192",
			raw:  "http://192.168.1.1:8000",
			want: false,
		},
		{
			name: "unspecified ipv4",
			raw:  "http://0.0.0.0:8000",
			want: false,
		},
		{
			name: "unspecified ipv6",
			raw:  "http://[::]:8000",
			want: false,
		},
		{
			name: "public",
			raw:  "https://api.example.com/v1",
			want: false,
		},
		{
			name: "public ipv4",
			raw:  "http://8.8.8.8",
			want: false,
		},
		{
			name: "localhost subdomain",
			raw:  "http://localhost.example.com",
			want: false,
		},
		{
			name: "localhost suffix",
			raw:  "http://notlocalhost",
			want: false,
		},
		{
			name: "empty",
			raw:  "",
			want: false,
		},
		{
			name: "space",
			raw:  " ",
			want: false,
		},
		{
			name: "malformed percent",
			raw:  "http://%",
			want: false,
		},
		{
			name: "relative",
			raw:  "/v1",
			want: false,
		},
		{
			name: "bare localhost",
			raw:  "localhost:8000",
			want: false,
		},
		{
			name: "file localhost",
			raw:  "file://localhost/tmp",
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLocalURL(tc.raw); got != tc.want {
				t.Fatalf("isLocalURL(%q)=%v want=%v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestClassifyProbeErrorBoundaryMatrix(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "401", err: &httpretry.APIError{StatusCode: 401, Message: "unauthorized"}, want: roleHealthAuth},
		{name: "403", err: &httpretry.APIError{StatusCode: 403, Message: "forbidden"}, want: roleHealthAuth},
		{name: "wrapped 401", err: fmt.Errorf("outer: %w", &httpretry.APIError{StatusCode: 401, Message: "bad key"}), want: roleHealthAuth},
		{name: "expired token", err: errors.New("token expired or incorrect"), want: roleHealthAuth},
		{name: "invalid api key", err: errors.New("invalid api key"), want: roleHealthAuth},
		{name: "connection refused", err: errors.New("dial tcp: connection refused"), want: roleHealthDown},
		{name: "timeout", err: errors.New("context deadline exceeded"), want: roleHealthDown},
		{name: "500", err: &httpretry.APIError{StatusCode: 500, Message: "boom"}, want: roleHealthDown},
		{name: "502", err: &httpretry.APIError{StatusCode: 502, Message: "bad gateway"}, want: roleHealthDown},
		{name: "429", err: &httpretry.APIError{StatusCode: 429, Message: "rate limit"}, want: roleHealthDown},
		{name: "empty", err: errors.New(""), want: roleHealthDown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProbeError(tc.err); got != tc.want {
				t.Fatalf("classify=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRoleHealthStateLoadSaveBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	w := &roleHealthWatch{logger: roleHealthTestLogger(), statePath: path, state: roleHealthState{LastProbeMs: 123, Verdicts: map[string]string{"a": "ok", "b": "auth"}}}
	w.saveState()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk roleHealthState
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.LastProbeMs != 123 || disk.Verdicts["a"] != "ok" || disk.Verdicts["b"] != "auth" {
		t.Errorf("disk=%+v", disk)
	}
	w2 := &roleHealthWatch{logger: roleHealthTestLogger(), statePath: path}
	w2.loadState()
	if w2.state.LastProbeMs != 123 || w2.state.Verdicts["b"] != "auth" {
		t.Errorf("loaded=%+v", w2.state)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	w3 := &roleHealthWatch{logger: roleHealthTestLogger(), statePath: path}
	w3.loadState()
	if w3.state.LastProbeMs != 0 || w3.state.Verdicts != nil {
		t.Errorf("corrupt loaded=%+v", w3.state)
	}
}

func TestVerdictsDefensiveCopyAndNilBoundary(t *testing.T) {
	var nilWatch *roleHealthWatch
	if got := nilWatch.Verdicts(); got != nil {
		t.Errorf("nil verdicts=%v", got)
	}
	w := &roleHealthWatch{state: roleHealthState{Verdicts: map[string]string{"p": roleHealthOK}}}
	a := w.Verdicts()
	b := w.Verdicts()
	a["p"] = roleHealthDown
	a["new"] = roleHealthAuth
	if b["p"] != roleHealthOK || len(b) != 1 {
		t.Errorf("copy changed=%v", b)
	}
	if got := w.Verdicts(); got["p"] != roleHealthOK || len(got) != 1 {
		t.Errorf("internal changed=%v", got)
	}
	if New(nil, nil, nil, "") != nil {
		t.Error("nil registry should disable watch")
	}
}

func TestRoleHealthTimingConstantsBoundaryInvariant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want time.Duration
	}{
		{name: "interval", got: roleHealthInterval, want: 6 * time.Hour},
		{name: "boot delay", got: roleHealthBootDelay, want: 2 * time.Minute},
		{name: "probe timeout", got: roleHealthProbeTimeout, want: 30 * time.Second},
		{name: "retry delay", got: roleHealthRetryDelay, want: 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got=%s want=%s", tc.got, tc.want)
			}
		})
	}
	if roleHealthBootDelay >= roleHealthInterval {
		t.Error("boot delay must be below interval")
	}
}

func TestVerdictsConcurrentCopies(t *testing.T) {
	w := &roleHealthWatch{state: roleHealthState{Verdicts: map[string]string{"provider": roleHealthOK}}}
	const workers = 128
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got := w.Verdicts()
				if got["provider"] != roleHealthOK {
					errs <- fmt.Errorf("worker=%d verdicts=%v", i, got)
					return
				}
				got["provider"] = roleHealthDown
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestApplyVerdictsEdgeBroadcastBoundary(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	var captured []map[string]any
	w := &roleHealthWatch{logger: logger, statePath: filepath.Join(t.TempDir(), "state.json"), broadcast: func(_ string, payload rtevents.EventPayload) {
		var m map[string]any
		_ = json.Unmarshal(payload.Bytes(), &m)
		captured = append(captured, m)
	}, state: roleHealthState{Verdicts: map[string]string{"p": roleHealthOK}}}
	target := roleHealthTarget{providerID: "p", model: "m", roles: []string{"main"}}
	w.applyVerdicts([]roleHealthTarget{target}, map[string]string{"p": roleHealthAuth}, map[string]string{"p": "expired"})
	if len(captured) != 1 || captured[0]["verdict"] != roleHealthAuth {
		t.Errorf("events=%v", captured)
	}
	w.applyVerdicts([]roleHealthTarget{target}, map[string]string{"p": roleHealthAuth}, map[string]string{"p": "still"})
	if len(captured) != 1 {
		t.Errorf("steady edge emitted events=%v", captured)
	}
	w.applyVerdicts([]roleHealthTarget{target}, map[string]string{"p": roleHealthOK}, map[string]string{})
	if len(captured) != 2 || captured[1]["verdict"] != roleHealthOK {
		t.Errorf("recovery events=%v", captured)
	}
	if !stringsContains(logs.String(), "unhealthy") || !stringsContains(logs.String(), "recovered") {
		t.Errorf("logs=%q", logs.String())
	}
}

func stringsContains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
