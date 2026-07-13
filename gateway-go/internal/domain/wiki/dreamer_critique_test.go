package wiki

import "testing"

func TestParseCritiqueDrops_DropAndKeep(t *testing.T) {
	resp := `[{"index":0,"verdict":"keep"},{"index":1,"verdict":"drop","reason":"중복"},{"index":2,"verdict":"DROP"}]`
	drop := parseCritiqueDrops(resp, 3, nil)
	if drop[0] {
		t.Error("index 0 marked keep must not drop")
	}
	if !drop[1] || !drop[2] {
		t.Errorf("expected 1,2 dropped (case-insensitive), got %v", drop)
	}
}

func TestParseCritiqueDrops_CodeFencedAndSalvaged(t *testing.T) {
	// Fenced + a truncated tail: the complete prefix objects still decode, and a
	// damaged final object is discarded rather than failing the whole verdict.
	resp := "```json\n[{\"index\":0,\"verdict\":\"drop\"},{\"index\":1,\"verd"
	drop := parseCritiqueDrops(resp, 2, nil)
	if !drop[0] {
		t.Errorf("salvaged prefix should drop index 0, got %v", drop)
	}
}

func TestParseCritiqueDrops_FailOpen(t *testing.T) {
	// Total garbage → keep everything (empty drop set), never zero the cycle.
	if got := parseCritiqueDrops("not json at all", 3, nil); len(got) != 0 {
		t.Errorf("unparseable critique must keep all, got %v", got)
	}
}

func TestParseCritiqueDrops_IgnoresOutOfRange(t *testing.T) {
	resp := `[{"index":7,"verdict":"drop"},{"index":-1,"verdict":"drop"}]`
	if got := parseCritiqueDrops(resp, 3, nil); len(got) != 0 {
		t.Errorf("out-of-range indices must be ignored, got %v", got)
	}
}
