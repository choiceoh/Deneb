package process

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// errCode extracts the protocol error code from an rpcerr.Error, failing the
// test if err is not one.
func errCode(t *testing.T, err error) string {
	t.Helper()
	var re *rpcerr.Error
	if !errors.As(err, &re) {
		t.Fatalf("error %v is not an *rpcerr.Error", err)
	}
	return re.Code
}

// ─── resolveJobID (id/jobId precedence) ───────────────────────────────────────

func TestResolveJobID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		jobID   string
		want    string
		wantErr string // "" means no error
	}{
		{name: "id wins over jobId", id: "a", jobID: "b", want: "a"},
		{name: "id only", id: "a", jobID: "", want: "a"},
		{name: "jobId fallback", id: "", jobID: "b", want: "b"},
		{name: "both empty errors", id: "", jobID: "", wantErr: protocol.ErrMissingParam},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveJobID(tt.id, tt.jobID)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveJobID(%q,%q) = %q, want error %s", tt.id, tt.jobID, got, tt.wantErr)
				}
				if code := errCode(t, err); code != tt.wantErr {
					t.Fatalf("error code = %q, want %q", code, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveJobID(%q,%q) unexpected error: %v", tt.id, tt.jobID, err)
			}
			if got != tt.want {
				t.Fatalf("resolveJobID(%q,%q) = %q, want %q", tt.id, tt.jobID, got, tt.want)
			}
		})
	}
}

// ─── ACP enabled gate ─────────────────────────────────────────────────────────

func TestRequireEnabled(t *testing.T) {
	deps := &ACPDeps{} // enabled defaults to false

	// Disabled → error frame carrying FEATURE_DISABLED.
	resp := requireEnabled(deps, "req-1")
	if resp == nil {
		t.Fatal("requireEnabled returned nil while disabled, want an error frame")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrFeatureDisabled {
		t.Fatalf("error = %+v, want code %s", resp.Error, protocol.ErrFeatureDisabled)
	}

	// Enabled → nil (no short-circuit).
	deps.SetEnabled(true)
	if resp := requireEnabled(deps, "req-2"); resp != nil {
		t.Fatalf("requireEnabled returned %+v while enabled, want nil", resp)
	}
}

func TestACPDeps_EnabledToggleAndLifecycle(t *testing.T) {
	deps := &ACPDeps{}
	if deps.IsEnabled() {
		t.Fatal("new ACPDeps should start disabled")
	}
	deps.SetEnabled(true)
	if !deps.IsEnabled() {
		t.Fatal("SetEnabled(true) not reflected by IsEnabled")
	}

	// acp.start / acp.stop must flip the same enabled flag the write gate reads.
	m := ACPMethods(deps)
	deps.SetEnabled(false)
	rpctest.MustOK(t, rpctest.Call(m, "acp.start", nil))
	if !deps.IsEnabled() {
		t.Fatal("acp.start did not enable ACP")
	}
	rpctest.MustOK(t, rpctest.Call(m, "acp.stop", nil))
	if deps.IsEnabled() {
		t.Fatal("acp.stop did not disable ACP")
	}
}

// TestACPWriteMethods_GatedWhenDisabled is the core permission-surface guard:
// every ACP write method must refuse with FEATURE_DISABLED before touching any
// dependency when ACP is disabled. A regression that drops a gate would let
// spawn/kill/send/bind run unauthenticated.
func TestACPWriteMethods_GatedWhenDisabled(t *testing.T) {
	deps := &ACPDeps{} // disabled
	m := ACPMethods(deps)

	writeMethods := []string{"acp.spawn", "acp.kill", "acp.send", "acp.bind", "acp.unbind"}
	for _, method := range writeMethods {
		t.Run(method, func(t *testing.T) {
			// Pass plausible params so the gate is what rejects, not validation.
			resp := rpctest.Call(m, method, map[string]any{
				"role":             "researcher",
				"agentId":          "agent-1",
				"message":          "hello",
				"targetSessionKey": "client:main",
				"bindingId":        "b-1",
			})
			if resp == nil {
				t.Fatalf("%s not registered", method)
			}
			if resp.Error == nil || resp.Error.Code != protocol.ErrFeatureDisabled {
				t.Fatalf("%s while disabled: error = %+v, want %s", method, resp.Error, protocol.ErrFeatureDisabled)
			}
		})
	}
}

// TestACPWriteMethods_PastGateValidation verifies argument validation downstream
// of the enable gate: once enabled, each method enforces its required params /
// dependencies rather than proceeding on empty input.
func TestACPWriteMethods_PastGateValidation(t *testing.T) {
	deps := &ACPDeps{}
	deps.SetEnabled(true)
	m := ACPMethods(deps)

	cases := []struct {
		method   string
		params   map[string]any
		wantCode string
	}{
		// Infra is nil → spawn cannot proceed even with a role.
		{"acp.spawn", map[string]any{"role": "researcher"}, protocol.ErrDependencyFailed},
		// Empty role short-circuits before infra (sanity: ordering).
		{"acp.kill", map[string]any{"agentId": ""}, protocol.ErrMissingParam},
		{"acp.send", map[string]any{"message": ""}, protocol.ErrMissingParam},
		// Bindings is nil → bind cannot proceed even with a target.
		{"acp.bind", map[string]any{"targetSessionKey": "client:main"}, protocol.ErrDependencyFailed},
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			resp := rpctest.Call(m, c.method, c.params)
			if resp == nil {
				t.Fatalf("%s not registered", c.method)
			}
			if resp.Error == nil || resp.Error.Code != c.wantCode {
				t.Fatalf("%s: error = %+v, want %s", c.method, resp.Error, c.wantCode)
			}
		})
	}
}

// ─── cron.update field merge ──────────────────────────────────────────────────

func newCronService(t *testing.T) *cron.Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	return cron.NewService(cron.ServiceConfig{StorePath: storePath}, nil, logger)
}

func TestCronUpdate_FieldMerge(t *testing.T) {
	svc := newCronService(t)
	const id = "job-1"
	orig := cron.StoreJob{
		ID:       id,
		Name:     "original",
		AgentID:  "agent-orig",
		Enabled:  true,
		Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 3_600_000},
		Payload:  cron.StorePayload{Kind: "agentTurn", Message: "orig command"},
	}
	if err := svc.Add(context.Background(), orig); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	var gotEvent string
	deps := CronAdvancedDeps{
		Service:     svc,
		Broadcaster: func(event string, _ any) (int, []error) { gotEvent = event; return 1, nil },
	}
	m := CronAdvancedMethods(deps)

	// Patch every mergeable field with well-typed values.
	resp := rpctest.Call(m, "cron.update", map[string]any{
		"id": id,
		"patch": map[string]any{
			"name":     "renamed",
			"enabled":  false,
			"command":  "new command",
			"agentId":  "agent-new",
			"schedule": "0 8 * * *", // valid cron → flips kind every→cron, proving re-parse
		},
	})
	rpctest.MustOK(t, resp)
	if gotEvent != "cron.changed" {
		t.Fatalf("broadcast event = %q, want cron.changed", gotEvent)
	}

	job := svc.Job(id)
	if job == nil {
		t.Fatal("job vanished after update")
	}
	if job.Name != "renamed" {
		t.Errorf("Name = %q, want renamed", job.Name)
	}
	if job.Enabled {
		t.Error("Enabled = true, want false after patch")
	}
	if job.Payload.Message != "new command" {
		t.Errorf("Payload.Message = %q, want 'new command'", job.Payload.Message)
	}
	if job.AgentID != "agent-new" {
		t.Errorf("AgentID = %q, want agent-new", job.AgentID)
	}
	if job.Schedule.Kind != "cron" || job.Schedule.Expr != "0 8 * * *" {
		t.Errorf("Schedule = %+v, want kind=cron expr=0 8 * * *", job.Schedule)
	}
}

// TestCronUpdate_IgnoresWrongTypesAndBadSchedule pins the defensive merge: a
// wrong-typed patch value (string where a bool/string is expected) and an
// unparseable schedule are silently ignored rather than corrupting the job.
// This is exactly the arg-parsing path where a bug would let a malformed RPC
// flip a job's enabled state or wedge its schedule.
func TestCronUpdate_IgnoresWrongTypesAndBadSchedule(t *testing.T) {
	svc := newCronService(t)
	const id = "job-2"
	orig := cron.StoreJob{
		ID:       id,
		Name:     "keep",
		Enabled:  true,
		Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 3_600_000},
		Payload:  cron.StorePayload{Kind: "agentTurn", Message: "keep command"},
	}
	if err := svc.Add(context.Background(), orig); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	m := CronAdvancedMethods(CronAdvancedDeps{Service: svc})
	resp := rpctest.Call(m, "cron.update", map[string]any{
		"id": id,
		"patch": map[string]any{
			"enabled":  "yes",               // wrong type (string, not bool) → ignored
			"name":     123,                 // wrong type (number, not string) → ignored
			"schedule": "invalid junk here", // unparseable → ignored
		},
	})
	rpctest.MustOK(t, resp)

	job := svc.Job(id)
	if job == nil {
		t.Fatal("job vanished after update")
	}
	if !job.Enabled {
		t.Error("Enabled flipped by a non-bool patch value")
	}
	if job.Name != "keep" {
		t.Errorf("Name = %q, want unchanged 'keep'", job.Name)
	}
	if job.Schedule.Kind != "every" || job.Schedule.EveryMs != 3_600_000 {
		t.Errorf("Schedule mutated by an invalid spec: %+v", job.Schedule)
	}
}
