package toolmeta

import (
	"context"
	"strings"
	"testing"
)

func TestCollectorRoundTrip(t *testing.T) {
	c := NewCollector()
	if c.JSON() != nil {
		t.Fatal("empty collector must serialize to nil, not {}")
	}
	ctx := WithCollector(context.Background(), c)
	Set(ctx, "activatedTools", []string{"graphify"})
	Set(ctx, "promptguard", "role-switch")

	raw := c.JSON()
	var tools []string
	if !Get(raw, "activatedTools", &tools) || len(tools) != 1 || tools[0] != "graphify" {
		t.Fatalf("activatedTools round-trip failed: %s", raw)
	}
	var label string
	if !Get(raw, "promptguard", &label) || label != "role-switch" {
		t.Fatalf("promptguard round-trip failed: %s", raw)
	}
}

func TestSetWithoutCollectorIsNoOp(t *testing.T) {
	Set(context.Background(), "key", "value") // must not panic
	var nilC *Collector
	nilC.Set("key", "value") // nil receiver must not panic
	if nilC.JSON() != nil {
		t.Fatal("nil collector must serialize to nil")
	}
}

func TestGetRejectsMissingAndMalformed(t *testing.T) {
	var out string
	if Get(nil, "k", &out) {
		t.Error("nil metadata must not report a hit")
	}
	if Get([]byte(`not json`), "k", &out) {
		t.Error("malformed metadata must not report a hit")
	}
	if Get([]byte(`{"other":1}`), "k", &out) {
		t.Error("absent key must not report a hit")
	}
	if Get([]byte(`{"k":123}`), "k", &out) {
		t.Error("type-mismatched value must not report a hit")
	}
}

func TestUnmarshalableValueDroppedWithoutBreakingRoundTrip(t *testing.T) {
	c := NewCollector()
	c.Set("bad", func() {}) // unmarshalable — dropped silently
	if c.JSON() != nil {
		t.Fatal("unmarshalable value must not create metadata")
	}
	if !strings.Contains(string(mustJSON(c, t, "ok", 1)), `"ok":1`) {
		t.Fatal("good value must still round-trip after a dropped one")
	}
}

func mustJSON(c *Collector, t *testing.T, k string, v any) []byte {
	t.Helper()
	c.Set(k, v)
	raw := c.JSON()
	if raw == nil {
		t.Fatal("expected metadata JSON")
	}
	return raw
}
