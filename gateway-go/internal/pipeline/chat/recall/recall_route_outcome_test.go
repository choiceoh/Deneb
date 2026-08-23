package recall

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func routingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// A hint that fired and a turn that called the tool it pointed at → followed.
func TestRecordRoutingOutcomeFollowed(t *testing.T) {
	var buf bytes.Buffer
	appendRoutingHint("block", "SunKean 견적 총액 얼마였지?", "s1", routingLogger(&buf))
	RecordRoutingOutcome("s1", []string{"wiki", "deal_ledger"}, routingLogger(&buf))

	out := buf.String()
	if !strings.Contains(out, "followed=true") || !strings.Contains(out, "tool=deal_ledger") {
		t.Fatalf("log = %q", out)
	}
}

// The same hint with a turn that only read pages → not followed. This is the
// case the whole loop exists to make visible.
func TestRecordRoutingOutcomeNotFollowed(t *testing.T) {
	var buf bytes.Buffer
	appendRoutingHint("block", "SunKean 견적 총액 얼마였지?", "s1", routingLogger(&buf))
	RecordRoutingOutcome("s1", []string{"wiki"}, routingLogger(&buf))

	if out := buf.String(); !strings.Contains(out, "followed=false") {
		t.Fatalf("log = %q", out)
	}
}

// Consume-once: a second turn must not inherit the first turn's expectation.
func TestRoutingOutcomeIsConsumedOnce(t *testing.T) {
	var buf bytes.Buffer
	appendRoutingHint("block", "SunKean 견적 총액 얼마였지?", "s1", routingLogger(&buf))
	RecordRoutingOutcome("s1", []string{"deal_ledger"}, routingLogger(&buf))
	buf.Reset()

	RecordRoutingOutcome("s1", []string{"wiki"}, routingLogger(&buf))
	if out := buf.String(); strings.Contains(out, "recall routing outcome") {
		t.Fatalf("second turn inherited the parked shape: %q", out)
	}
}

// A turn whose message matches no shape clears the slot, so an unrelated later
// answer is never scored against a stale hint.
func TestHintlessTurnClearsTheParkedShape(t *testing.T) {
	var buf bytes.Buffer
	appendRoutingHint("block", "SunKean 견적 총액 얼마였지?", "s1", routingLogger(&buf))
	appendRoutingHint("block", "오늘 일정 알려줘", "s1", routingLogger(&buf))
	buf.Reset()

	RecordRoutingOutcome("s1", []string{"wiki"}, routingLogger(&buf))
	if out := buf.String(); strings.Contains(out, "recall routing outcome") {
		t.Fatalf("stale shape survived a hintless turn: %q", out)
	}
}

func TestDedupeToolsKeepsCallOrder(t *testing.T) {
	got := dedupeTools([]string{"wiki", "wiki", "", "deal_ledger", "wiki"})
	if strings.Join(got, ",") != "wiki,deal_ledger" {
		t.Fatalf("dedupeTools = %v", got)
	}
}
