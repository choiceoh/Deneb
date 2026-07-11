package briefcase

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

// ApprovalFunc is invoked only for signed tool rules whose decision is
// approval. A nil callback denies every such call.
type ApprovalFunc func(name, toolCallID string, input []byte) bool

type ToolCallRecord struct {
	Name        string `json:"name"`
	ToolCallID  string `json:"toolCallId"`
	InputSHA256 string `json:"inputSha256"`
	Decision    string `json:"decision"`
}

// ToolGate enforces a signed case tool policy at the Handler's BeforeToolCall
// boundary. It reserves budget before execution, so errors and retries still
// count as attempts and cannot be used to bypass MaxCalls.
type ToolGate struct {
	mu       sync.Mutex
	policy   casepack.ToolPolicy
	rules    map[string]casepack.ToolRule
	approval ApprovalFunc
	total    int
	perTool  map[string]int
	seenIDs  map[string]string
	records  []ToolCallRecord
}

func NewToolGate(policy casepack.ToolPolicy, approval ApprovalFunc) (*ToolGate, error) {
	if policy.Default != casepack.ToolDeny || policy.MaxCalls <= 0 {
		return nil, fmt.Errorf("briefcase: invalid fail-closed tool policy")
	}
	rules := make(map[string]casepack.ToolRule, len(policy.Rules))
	for _, rule := range policy.Rules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			return nil, fmt.Errorf("briefcase: blank tool rule")
		}
		if _, exists := rules[name]; exists {
			return nil, fmt.Errorf("briefcase: duplicate tool rule %q", name)
		}
		rules[name] = rule
	}
	return &ToolGate{
		policy:   policy,
		rules:    rules,
		approval: approval,
		perTool:  make(map[string]int),
		seenIDs:  make(map[string]string),
	}, nil
}

// BeforeToolCall has the exact shape required by chat.SyncOptions.
func (g *ToolGate) BeforeToolCall(name, toolCallID string, input []byte) (bool, string) {
	if g == nil {
		return true, "briefcase tool gate is unavailable"
	}
	name = strings.TrimSpace(name)
	toolCallID = strings.TrimSpace(toolCallID)
	sum := sha256.Sum256(input)
	fingerprint := name + ":" + hex.EncodeToString(sum[:])

	g.mu.Lock()
	defer g.mu.Unlock()
	// Reserve every attempted call before validating it. Malformed calls,
	// duplicate IDs, policy denials, and failed approvals all consume the same
	// signed global attempt budget as an allowed execution.
	g.total++
	if g.total > g.policy.MaxCalls {
		g.record(name, toolCallID, sum, "global_budget_exhausted")
		return true, "briefcase global tool-call budget exhausted"
	}
	if name == "" || toolCallID == "" {
		g.record(name, toolCallID, sum, "invalid_call")
		return true, "briefcase tool name and call id are required"
	}
	if prior, exists := g.seenIDs[toolCallID]; exists {
		if prior == fingerprint {
			g.record(name, toolCallID, sum, "duplicate")
			return true, "briefcase duplicate tool call id"
		}
		g.record(name, toolCallID, sum, "call_id_reused")
		return true, "briefcase tool call id was reused with different input"
	}
	g.seenIDs[toolCallID] = fingerprint
	rule, ok := g.rules[name]
	if !ok || rule.Decision == casepack.ToolDeny {
		g.record(name, toolCallID, sum, "denied")
		return true, fmt.Sprintf("tool %q is denied by the signed briefcase policy", name)
	}
	if rule.MaxCalls > 0 && g.perTool[name] >= rule.MaxCalls {
		g.record(name, toolCallID, sum, "tool_budget_exhausted")
		return true, fmt.Sprintf("briefcase tool-call budget exhausted for %q", name)
	}
	if rule.Decision == casepack.ToolApproval && (g.approval == nil || !g.approval(name, toolCallID, input)) {
		g.record(name, toolCallID, sum, "approval_denied")
		return true, fmt.Sprintf("tool %q requires briefcase approval", name)
	}
	if rule.Decision != casepack.ToolAllow && rule.Decision != casepack.ToolApproval {
		g.record(name, toolCallID, sum, "invalid_policy")
		return true, fmt.Sprintf("tool %q has an invalid briefcase decision", name)
	}

	g.perTool[name]++
	g.record(name, toolCallID, sum, "allowed")
	return false, ""
}

// RemainingAttempts returns the unreserved portion of the signed case-wide
// tool-call attempt budget. It is safe to snapshot before starting one
// synchronous agent run; the Briefcase harness serializes those runs.
func (g *ToolGate) RemainingAttempts() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	remaining := g.policy.MaxCalls - g.total
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (g *ToolGate) Records() []ToolCallRecord {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ToolCallRecord, len(g.records))
	copy(out, g.records)
	return out
}

func (g *ToolGate) record(name, id string, sum [sha256.Size]byte, decision string) {
	g.records = append(g.records, ToolCallRecord{
		Name: name, ToolCallID: id, InputSHA256: hex.EncodeToString(sum[:]), Decision: decision,
	})
}
