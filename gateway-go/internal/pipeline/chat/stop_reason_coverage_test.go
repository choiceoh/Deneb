package chat

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every stop reason the agent can emit must resolve to a decision here: a
// user-facing fallback, or an explicit entry in the intentionally-silent set.
// The default arm returns "" — silence — so a stop reason that lands there by
// omission delivers nothing at all, with ok=true.
//
// That is exactly what happened to max_total_tokens: its siblings max_turns and
// max_turns_graceful were in the table from the start, it was added to the
// executor later, and a budget-exhausted run with no prose yet (the ordinary
// shape for a tool-heavy automation turn) delivered an empty reply.
func TestFallbackCoversEveryStopReasonTheAgentEmits(t *testing.T) {
	// Deliberate silences, each for its own reason:
	//   end_turn      — a tool-only turn legitimately ends with no text, and
	//                   isEmptyFinalResult (run_fallback.go) owns the accidental
	//                   case so intentional NO_REPLY is never overwritten.
	//   steered       — a synthetic SyncResult carrying its own ack text; the
	//   slash_command   agent loop never ran, so the result is never empty and
	//                   deliverEmptyRunReply is unreachable for both.
	// Adding a reason here is a claim that the run always carries its own text.
	intentionallySilent := map[string]bool{
		"end_turn":      true,
		"steered":       true,
		"slash_command": true,
	}

	roots := []string{
		filepath.Join("..", "..", "ai", "agent"),
		".",
	}
	literal := regexp.MustCompile(`StopReason\s*[:=]+\s*"([a-z_]+)"`)
	fromCtx := regexp.MustCompile(`return\s+"(timeout|aborted)"`)
	constDecl := regexp.MustCompile(`StopReason\w*\s*=\s*"([a-z_]+)"`)

	emitted := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			body := string(raw)
			for _, re := range []*regexp.Regexp{literal, constDecl} {
				for _, m := range re.FindAllStringSubmatch(body, -1) {
					emitted[m[1]] = true
				}
			}
			if strings.Contains(body, "func stopReasonFromCtx") {
				for _, m := range fromCtx.FindAllStringSubmatch(body, -1) {
					emitted[m[1]] = true
				}
			}
		}
	}
	if len(emitted) < 4 {
		t.Fatalf("scan found only %d stop reasons (%v); the scan is broken, not the code", len(emitted), emitted)
	}

	for reason := range emitted {
		if intentionallySilent[reason] {
			if got := fallbackForStopReason(reason); got != "" {
				t.Errorf("stop reason %q is meant to stay silent but returned %q", reason, got)
			}
			continue
		}
		if got := fallbackForStopReason(reason); got == "" {
			t.Errorf("stop reason %q has no user-facing fallback; it lands in the default arm and delivers nothing", reason)
		}
	}
}

// The known reasons keep their distinct wording — a copy-paste that collapses
// two causes into one message would pass the coverage test above.
func TestStopReasonFallbacksAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, reason := range []string{
		"max_turns", "max_turns_graceful", "max_total_tokens",
		"timeout", "aborted", "error", stopReasonCompressionStuck,
	} {
		msg := fallbackForStopReason(reason)
		if msg == "" {
			t.Errorf("%s: empty fallback", reason)
			continue
		}
		if prev, dup := seen[msg]; dup {
			t.Errorf("%s and %s share the same message; the cause is no longer distinguishable", reason, prev)
		}
		seen[msg] = reason
	}
}
