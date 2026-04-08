package shortid

import (
	"strings"
	"testing"
)

func TestNew_Format(t *testing.T) {
	id := New("run")
	if !strings.HasPrefix(id, "run_") {
		t.Fatalf("got %s, want prefix run_", id)
	}
	// "run_" (4) + 4 digits = 8
	if len(id) != len("run_")+4 {
		t.Fatalf("got %d (%s), want len %d", len(id), id, len("run_")+4)
	}
	t.Logf("generated id: %s", id)
}

func TestNew_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := range 1000 {
		id := New("x")
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNew_Wrap(t *testing.T) {
	// Force counter near wrap point.
	counter.Store(9998)
	a := New("p")
	b := New("p")
	c := New("p")
	if a != "p_9998" {
		t.Fatalf("got %s, want p_9998", a)
	}
	if b != "p_9999" {
		t.Fatalf("got %s, want p_9999", b)
	}
	if c != "p_0000" {
		t.Fatalf("got %s, want p_0000", c)
	}
}
