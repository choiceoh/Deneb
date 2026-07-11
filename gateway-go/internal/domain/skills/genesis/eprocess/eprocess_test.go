package eprocess

import (
	"encoding/json"
	"testing"
)

// Table-driven behavior spec for the anytime-valid primitive. The formal
// validity guarantee is mathematical (supermartingale + Ville); these tables
// pin the implementation to it deterministically.
func TestEProcess_Tables(t *testing.T) {
	t.Run("healthy stream never rejects", func(t *testing.T) {
		// Baseline 0.25, observed stream at exactly baseline cadence.
		p := NewEProcess(0.05, 0.25)
		for i := 0; i < 200; i++ {
			p.Observe(i%4 == 0) // 1 fail per 4 uses
		}
		if p.Reject() {
			t.Fatalf("healthy stream rejected: E=%v after N=%d", p.E, p.N)
		}
	})
	t.Run("clear degradation rejects quickly", func(t *testing.T) {
		p := NewEProcess(0.05, 0.10)
		n := 0
		for !p.Reject() {
			p.Observe(true)
			n++
			if n > 20 {
				t.Fatalf("degradation not detected within 20 fails: E=%v", p.E)
			}
		}
		if n < 3 {
			t.Fatalf("rejected suspiciously fast (n=%d) — betting too aggressive", n)
		}
	})
	t.Run("improvement drives evidence down", func(t *testing.T) {
		p := NewEProcess(0.05, 0.5)
		for i := 0; i < 50; i++ {
			p.Observe(false)
		}
		if p.E >= 1 || p.Reject() {
			t.Fatalf("all-success stream should shrink evidence: E=%v", p.E)
		}
	})
	t.Run("clamps and alpha fallback", func(t *testing.T) {
		p := NewEProcess(0, 0) // degenerate inputs
		if p.Alpha != DefaultEProcessAlpha || p.Baseline != eProcessBaselineFloor {
			t.Fatalf("clamps: %+v", p)
		}
		if q := NewEProcess(0.05, 1); q.Baseline != eProcessBaselineCeil {
			t.Fatalf("ceil clamp: %+v", q)
		}
	})
	t.Run("state survives serialization", func(t *testing.T) {
		p := NewEProcess(0.05, 0.2)
		p.Observe(true)
		p.Observe(false)
		raw, _ := json.Marshal(p)
		var q EProcess
		if err := json.Unmarshal(raw, &q); err != nil {
			t.Fatal(err)
		}
		if q.E != p.E || q.N != p.N || q.Baseline != p.Baseline {
			t.Fatalf("round trip lost state: %+v vs %+v", q, p)
		}
	})
}
