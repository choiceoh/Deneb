package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// TestExecute_DryRunSuppressesSideEffectTools verifies the default-deny
// polarity: allowlisted read-only tools execute normally under dry-run, every
// other tool returns a stub without its fn being invoked.
func TestExecute_DryRunSuppressesSideEffectTools(t *testing.T) {
	reg := NewToolRegistry()
	executed := map[string]int{}
	fake := func(name string) ToolFunc {
		return func(_ context.Context, _ json.RawMessage) (string, error) {
			executed[name]++
			return name + " ran", nil
		}
	}
	for _, name := range []string{"grep", "read", "write", "exec", "message", "unregistered_new_tool"} {
		reg.Register(name, fake(name))
	}

	ctx := toolport.WithToolDryRun(context.Background())

	// Allowlisted tools run for real.
	for _, name := range []string{"grep", "read"} {
		out, err := reg.Execute(ctx, name, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if out != name+" ran" || executed[name] != 1 {
			t.Fatalf("%s: out=%q executed=%d, want real execution", name, out, executed[name])
		}
	}

	// Side-effect tools — including a tool the allowlist has never heard of —
	// are stubbed and never invoked.
	for _, name := range []string{"write", "exec", "message", "unregistered_new_tool"} {
		out, err := reg.Execute(ctx, name, json.RawMessage(`{"file_path":"x"}`))
		if err != nil {
			t.Fatalf("%s: dry-run stub must not error: %v", name, err)
		}
		if !strings.Contains(out, "[dry-run]") || !strings.Contains(out, name) {
			t.Fatalf("%s: out=%q, want dry-run stub naming the tool", name, out)
		}
		if executed[name] != 0 {
			t.Fatalf("%s executed %d times under dry-run, want 0", name, executed[name])
		}
	}

	// Without the flag the same registry executes normally.
	if _, err := reg.Execute(context.Background(), "write", json.RawMessage(`{"file_path":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if executed["write"] != 1 {
		t.Fatalf("write executed %d times without dry-run, want 1", executed["write"])
	}
}

func TestToolDryRunContext(t *testing.T) {
	if toolport.ToolDryRunFromContext(context.Background()) {
		t.Fatal("dry-run must default to false")
	}
	if !toolport.ToolDryRunFromContext(toolport.WithToolDryRun(context.Background())) {
		t.Fatal("dry-run flag not carried by context")
	}
}

// TestExecute_DryRunKeepsVerifyGateFaithful: a stubbed write must arm the
// verify gate and a stubbed verification exec must disarm it, mirroring a
// real run so replayed edit flows still see the finalize nudge.
func TestExecute_DryRunKeepsVerifyGateFaithful(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"write", "exec"} {
		reg.Register(name, func(_ context.Context, _ json.RawMessage) (string, error) {
			t.Fatalf("%s must not execute under dry-run", name)
			return "", nil
		})
	}

	gate := &verifyGateState{}
	ctx := WithVerifyGate(toolport.WithToolDryRun(context.Background()), gate)

	if _, err := reg.Execute(ctx, "write", json.RawMessage(`{"file_path":"a.go","content":"x"}`)); err != nil {
		t.Fatal(err)
	}
	gate.mu.Lock()
	armed := gate.mutated
	gate.mu.Unlock()
	if !armed {
		t.Fatal("stubbed write must arm the verify gate")
	}

	if _, err := reg.Execute(ctx, "exec", json.RawMessage(`{"command":"go test ./..."}`)); err != nil {
		t.Fatal(err)
	}
	gate.mu.Lock()
	armed = gate.mutated
	gate.mu.Unlock()
	if armed {
		t.Fatal("stubbed verification exec must disarm the verify gate")
	}
}
