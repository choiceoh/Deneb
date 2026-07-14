package vectorutil

import (
	"math"
	"testing"
)

func TestCosineUsesFallbackZeroForDegenerateVectors(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{name: "same", a: []float32{1, 2}, b: []float32{1, 2}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "opposite", a: []float32{1, 0}, b: []float32{-1, 0}, want: -1},
		{name: "empty", want: 0},
		{name: "mismatch", a: []float32{1}, b: []float32{1, 2}, want: 0},
		{name: "zero norm", a: []float32{0, 0}, b: []float32{1, 2}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Cosine(test.a, test.b); math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("Cosine(%v, %v) = %v, want %v", test.a, test.b, got, test.want)
			}
		})
	}
}
