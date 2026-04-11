package metrics

import (
	"testing"
)

func TestCounterInc(t *testing.T) {
	c := NewCounter()
	c.Inc("foo", "ok")
	c.Inc("foo", "ok")
	c.Inc("foo", "error")

	snap := c.Snapshot()
	if snap["foo\x00ok"] != 2 {
		t.Errorf("expected foo/ok=2, got %d", snap["foo\x00ok"])
	}
	if snap["foo\x00error"] != 1 {
		t.Errorf("expected foo/error=1, got %d", snap["foo\x00error"])
	}
}

func TestCounterAdd(t *testing.T) {
	c := NewCounter()
	c.Inc("input")
	c.Inc("input")
	c.Inc("input")
	c.Inc("output")

	snap := c.Snapshot()
	if snap["input"] != 3 {
		t.Errorf("expected input=3, got %d", snap["input"])
	}
	if snap["output"] != 1 {
		t.Errorf("expected output=1, got %d", snap["output"])
	}
}

func TestCounterSnapshot(t *testing.T) {
	c := NewCounter()
	c.Inc("a")
	c.Inc("b")

	snap := c.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 entries, got %d", len(snap))
	}
}
