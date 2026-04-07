package shortid

import (
	"strings"
	"testing"
)

func TestNew_Format(t *testing.T) {
	id := New("run")
	if !strings.HasPrefix(id, "run_") {
		t.Fatalf("expected prefix run_, got %s", id)
	}
	// "run_" (4) + 4 digits = 8
	if len(id) != len("run_")+4 {
		t.Fatalf("expected len %d, got %d (%s)", len("run_")+4, len(id), id)
	}
	t.Logf("generated id: %s", id)
}

func TestNew_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
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
		t.Fatalf("expected p_9998, got %s", a)
	}
	if b != "p_9999" {
		t.Fatalf("expected p_9999, got %s", b)
	}
	if c != "p_0000" {
		t.Fatalf("expected p_0000, got %s", c)
	}
}
