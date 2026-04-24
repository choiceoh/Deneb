package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// TestConsoleHandler_NilReplaceAttr_ByteIdentical verifies that a nil
// ReplaceAttr produces the same output as before the option existed —
// the no-color basic format must match exactly.
func TestConsoleHandler_NilReplaceAttr_ByteIdentical(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "server started",
		slog.String("addr", "127.0.0.1:8080"),
		slog.Int("port", 8080),
	)
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	want := "14:05:09 INF │ server started addr=127.0.0.1:8080 port=8080\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestConsoleHandler_ReplaceAttr_MasksStringValues confirms that a replacer
// which rewrites string values is reflected in the rendered output.
func TestConsoleHandler_ReplaceAttr_MasksStringValues(t *testing.T) {
	var buf bytes.Buffer
	mask := func(groups []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, "***")
		}
		return a
	}
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: mask,
	})

	r := newTestRecord(slog.LevelInfo, "request",
		slog.String("token", "secret-xyz"),
		slog.Int("port", 8080),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, "token=***") {
		t.Errorf("expected token=***, got %q", got)
	}
	if strings.Contains(got, "secret-xyz") {
		t.Errorf("raw secret leaked into output: %q", got)
	}
	if !strings.Contains(got, "port=8080") {
		t.Errorf("non-string attr should be untouched: %q", got)
	}
}

// TestConsoleHandler_ReplaceAttr_DropsZero confirms that returning a zero
// Attr causes the attribute to be omitted entirely from the output.
func TestConsoleHandler_ReplaceAttr_DropsZero(t *testing.T) {
	var buf bytes.Buffer
	drop := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "drop_me" {
			return slog.Attr{}
		}
		return a
	}
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: drop,
	})

	r := newTestRecord(slog.LevelInfo, "event",
		slog.String("keep", "yes"),
		slog.String("drop_me", "secret"),
		slog.Int("port", 8080),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if strings.Contains(got, "drop_me") {
		t.Errorf("drop_me key should be absent: %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Errorf("dropped value should not appear: %q", got)
	}
	if !strings.Contains(got, "keep=yes") {
		t.Errorf("kept attr missing: %q", got)
	}
	if !strings.Contains(got, "port=8080") {
		t.Errorf("unrelated attr missing: %q", got)
	}
}

// TestConsoleHandler_ReplaceAttr_GroupsThreaded verifies that an attribute
// nested inside a slog.Group reaches the callback with the correct groups
// ancestry (matching slog.JSONHandler semantics).
func TestConsoleHandler_ReplaceAttr_GroupsThreaded(t *testing.T) {
	var buf bytes.Buffer
	var sawGroups [][]string
	capture := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "nested" {
			// Copy — slog explicitly forbids retaining the groups slice.
			gs := make([]string, len(groups))
			copy(gs, groups)
			sawGroups = append(sawGroups, gs)
		}
		return a
	}
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: capture,
	})

	r := newTestRecord(slog.LevelInfo, "event",
		slog.Group("outer",
			slog.String("nested", "value"),
		),
	)
	h.Handle(context.TODO(), r)

	if len(sawGroups) != 1 {
		t.Fatalf("expected exactly 1 call for key 'nested', got %d (%v)", len(sawGroups), sawGroups)
	}
	want := []string{"outer"}
	if !equalStrings(sawGroups[0], want) {
		t.Errorf("groups arg: got %v want %v", sawGroups[0], want)
	}
}

// TestConsoleHandler_ReplaceAttr_NestedGroups verifies the groups stack is
// deepened correctly for groups within groups.
func TestConsoleHandler_ReplaceAttr_NestedGroups(t *testing.T) {
	var buf bytes.Buffer
	var sawGroups [][]string
	capture := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "leaf" {
			gs := make([]string, len(groups))
			copy(gs, groups)
			sawGroups = append(sawGroups, gs)
		}
		return a
	}
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: capture,
	})

	r := newTestRecord(slog.LevelInfo, "event",
		slog.Group("outer",
			slog.Group("inner",
				slog.String("leaf", "hi"),
			),
		),
	)
	h.Handle(context.TODO(), r)

	if len(sawGroups) != 1 {
		t.Fatalf("expected 1 capture, got %d (%v)", len(sawGroups), sawGroups)
	}
	want := []string{"outer", "inner"}
	if !equalStrings(sawGroups[0], want) {
		t.Errorf("groups arg: got %v want %v", sawGroups[0], want)
	}
}

// TestConsoleHandler_ReplaceAttr_WithGroupHandler verifies that groups added
// via handler.WithGroup are visible to the replacer for subsequent attrs.
func TestConsoleHandler_ReplaceAttr_WithGroupHandler(t *testing.T) {
	var buf bytes.Buffer
	var sawGroups [][]string
	capture := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "status" {
			gs := make([]string, len(groups))
			copy(gs, groups)
			sawGroups = append(sawGroups, gs)
		}
		return a
	}
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: capture,
	})
	h2 := h.WithGroup("http")

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("status", 200))
	h2.Handle(context.TODO(), r)

	if len(sawGroups) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(sawGroups))
	}
	want := []string{"http"}
	if !equalStrings(sawGroups[0], want) {
		t.Errorf("groups arg: got %v want %v", sawGroups[0], want)
	}
}

// TestConsoleHandler_ReplaceAttr_RedactIntegration wires redact.AttrReplacer
// in and confirms a runtime-assembled token is masked end-to-end.
func TestConsoleHandler_ReplaceAttr_RedactIntegration(t *testing.T) {
	if !redact.Enabled() {
		t.Skip("DENEB_REDACT_SECRETS disabled at package init — cannot test integration")
	}
	// Build a synthetic token at runtime so the source file contains no
	// literal secret prefix.
	token := "sk-proj-" + strings.Repeat("Z", 24)

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: redact.AttrReplacer(nil),
	})

	r := newTestRecord(slog.LevelInfo, "auth",
		slog.String("api_key", token),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if strings.Contains(got, token) {
		t.Errorf("raw token leaked into console output: %q", got)
	}
	// redact.maskToken preserves the first 6 and last 4 characters joined by
	// "...". The first 6 of our synthetic token are "sk-pro".
	if !strings.Contains(got, "sk-pro...") {
		t.Errorf("expected masked prefix 'sk-pro...' in output: %q", got)
	}
	// The secret body (24 Zs) must not appear whole.
	if strings.Contains(got, strings.Repeat("Z", 24)) {
		t.Errorf("secret body leaked: %q", got)
	}
}

// TestConsoleHandler_ReplaceAttr_PriorReplacerChain verifies a downstream
// replacer sees the Attr AFTER the prior one has been applied (matches the
// redact.AttrReplacer(prev) composition contract).
func TestConsoleHandler_ReplaceAttr_PriorReplacerChain(t *testing.T) {
	first := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "stage" {
			return slog.String(a.Key, "mid-"+a.Value.String())
		}
		return a
	}
	second := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "stage" && a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, a.Value.String()+"-final")
		}
		return a
	}
	composed := func(groups []string, a slog.Attr) slog.Attr {
		return second(groups, first(groups, a))
	}

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: composed,
	})

	r := newTestRecord(slog.LevelInfo, "pipeline",
		slog.String("stage", "start"),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, "stage=mid-start-final") {
		t.Errorf("chained replacers not applied in order: %q", got)
	}
}

// TestConsoleHandler_ReplaceAttr_GroupAttrNotPassed confirms that a
// Group-kind Attr itself is not handed to the replacer — only its contents.
// (Matches slog.JSONHandler.)
func TestConsoleHandler_ReplaceAttr_GroupAttrNotPassed(t *testing.T) {
	var seenKeys []string
	capture := func(groups []string, a slog.Attr) slog.Attr {
		seenKeys = append(seenKeys, a.Key)
		return a
	}
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{
		Level:       slog.LevelDebug,
		Color:       false,
		ReplaceAttr: capture,
	})

	r := newTestRecord(slog.LevelInfo, "event",
		slog.Group("grp",
			slog.String("child", "v"),
		),
	)
	h.Handle(context.TODO(), r)

	// Only "child" should appear — "grp" itself must NOT be presented.
	for _, k := range seenKeys {
		if k == "grp" {
			t.Errorf("group attr key was passed to replacer (should not be): %v", seenKeys)
		}
	}
	found := false
	for _, k := range seenKeys {
		if k == "child" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("child attr was not passed to replacer: %v", seenKeys)
	}
}

// equalStrings compares two string slices for exact equality.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
