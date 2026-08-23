package mcpclient

import (
	"os"
	"strings"
	"testing"
)

func TestStderrDeduperCollapsesRepeatsAndDigitVariants(t *testing.T) {
	d := newStderrDeduper()
	if n := d.observe(`[cause]: Error: getaddrinfo EAI_AGAIN us.i.posthog.com`); n != 1 {
		t.Fatalf("first sight = %d, want 1", n)
	}
	if n := d.observe(`[cause]: Error: getaddrinfo EAI_AGAIN us.i.posthog.com`); n != 2 {
		t.Fatalf("repeat = %d, want 2", n)
	}
	// Timestamp/pid variants of one message share a key.
	a := d.observe(`{"level":30,"time":1787000000001,"pid":4311,"event":"tool_call_error"}`)
	b := d.observe(`{"level":30,"time":1787000000999,"pid":4312,"event":"tool_call_error"}`)
	if a != 1 || b != 2 {
		t.Fatalf("digit variants did not collapse: %d, %d", a, b)
	}
	if d.observe("a different line") != 1 {
		t.Fatal("distinct line must start its own count")
	}
}

func TestStderrDeduperBoundsItsKeySet(t *testing.T) {
	d := newStderrDeduper()
	for i := 0; i < stderrDedupeMaxKeys+10; i++ {
		d.observe("line-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("y", i/26))
	}
	if len(d.counts) > stderrDedupeMaxKeys {
		t.Fatalf("key set grew past the bound: %d", len(d.counts))
	}
}

func TestChildEnvOptsOutOfTelemetryUnlessParentDecides(t *testing.T) {
	os.Unsetenv("DO_NOT_TRACK")
	var got string
	for _, kv := range childEnv() {
		if strings.HasPrefix(kv, "DO_NOT_TRACK=") {
			got = kv
		}
	}
	if got != "DO_NOT_TRACK=1" {
		t.Fatalf("default telemetry opt-out missing: %q", got)
	}
	t.Setenv("DO_NOT_TRACK", "0")
	got = ""
	for _, kv := range childEnv() {
		if strings.HasPrefix(kv, "DO_NOT_TRACK=") {
			got = kv
		}
	}
	if got != "DO_NOT_TRACK=0" {
		t.Fatalf("parent's explicit value must pass through: %q", got)
	}
}
