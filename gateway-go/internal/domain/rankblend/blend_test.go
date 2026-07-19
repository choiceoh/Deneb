package rankblend

import (
	"math"
	"reflect"
	"testing"
)

func TestBlendPreservesStrongRetrievalHead(t *testing.T) {
	result, ok := Blend(
		[]float64{1, 0.20, 0.10},
		[]float64{-10, -5, 1000},
		DefaultConfig,
	)
	if !ok {
		t.Fatal("Blend rejected valid scores")
	}
	if result.Order[0] != 0 || result.ChangedTop1 {
		t.Fatalf("strong retrieval head was displaced: %+v", result)
	}
}

func TestBlendUsesRerankerOrdinalNotRawScale(t *testing.T) {
	retrieval := []float64{1, 0.92, 0.85, 0.80}
	a, okA := Blend(retrieval, []float64{0.01, 0.02, 0.03, 0.04}, DefaultConfig)
	b, okB := Blend(retrieval, []float64{-900, -20, 3, 1e9}, DefaultConfig)
	if !okA || !okB {
		t.Fatal("Blend rejected finite score scales")
	}
	if !reflect.DeepEqual(a.Order, b.Order) || !reflect.DeepEqual(a.Scores, b.Scores) {
		t.Fatalf("raw score scale changed result: compressed=%+v extreme=%+v", a, b)
	}
}

func TestBlendOrderOnlyLetsRerankerCorrectCandidateOrder(t *testing.T) {
	result, ok := Blend(
		OrdinalScores(2),
		[]float64{0.1, 0.9},
		OrderOnlyConfig,
	)
	if !ok {
		t.Fatal("Blend rejected valid order-only scores")
	}
	if result.Order[0] != 1 || !result.ChangedTop1 {
		t.Fatalf("reranker could not correct order-only retrieval: %+v", result)
	}
}

func TestBlendFailsOpenOnMalformedScores(t *testing.T) {
	if _, ok := Blend([]float64{1, 0.5}, []float64{0, math.NaN()}, DefaultConfig); ok {
		t.Fatal("Blend accepted NaN")
	}
	if _, ok := Blend([]float64{1}, []float64{1}, DefaultConfig); ok {
		t.Fatal("Blend accepted one candidate")
	}
	if _, ok := Blend([]float64{1, 0.5}, []float64{1}, DefaultConfig); ok {
		t.Fatal("Blend accepted mismatched shapes")
	}
}

func TestOrdinalScores(t *testing.T) {
	got := OrdinalScores(4)
	want := []float64{1, 2.0 / 3, 1.0 / 3, 0}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("OrdinalScores = %v", got)
		}
	}
}
