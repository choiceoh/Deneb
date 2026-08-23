package recallops

import (
	"encoding/json"
	"strings"
)

// KnowledgeOpFromInput resolves the op a knowledge tool call selects, exactly as
// ToolKnowledge does: the `op` field, falling back to the `action` alias, with
// alias normalization applied. Gates outside this package must use it rather
// than reading `op` themselves — a gate that misses the alias (`action`,
// `write`, `쓰기`) lets through the very call it exists to stop.
//
// An unparsable payload yields "" — callers that gate writes should treat that
// as a write, since the tool itself will reject it anyway.
func KnowledgeOpFromInput(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var payload struct {
		Op     string `json:"op"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	op := normalizeKnowledgeOp(payload.Op)
	if op == "" {
		op = normalizeKnowledgeOp(payload.Action)
	}
	return op
}

// IsKnowledgeWriteOp reports whether an op persists anything: a wiki page or a
// canonical fact mutation.
func IsKnowledgeWriteOp(op string) bool {
	return strings.EqualFold(op, "record") || IsKnowledgeFactMutationOp(op)
}

// IsKnowledgeFactMutationOp reports whether an op appends to the canonical fact
// journal, which steers every later recall and cannot be un-appended.
func IsKnowledgeFactMutationOp(op string) bool {
	return strings.EqualFold(op, "assert_fact") || strings.EqualFold(op, "forget_fact")
}
