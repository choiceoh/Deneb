package server

import "testing"

func TestShouldWarnMemPressureIgnoresAllocSawtoothWhenRetainedHeapIsStable(t *testing.T) {
	const mib = uint64(1024 * 1024)
	previous := memPressureSnapshot{
		alloc:        400 * mib,
		retainedHeap: 1200 * mib,
	}
	current := memPressureSnapshot{
		alloc:        1100 * mib,
		retainedHeap: 1250 * mib,
	}

	if shouldWarnMemPressure(previous, current) {
		t.Fatal("normal post-GC Alloc rebound must not be reported as memory pressure")
	}
}

func TestShouldWarnMemPressureIgnoresRetainedHeapWarmupBelowActionableSize(t *testing.T) {
	const mib = uint64(1024 * 1024)
	previous := memPressureSnapshot{
		alloc:        700 * mib,
		retainedHeap: 700 * mib,
	}
	current := memPressureSnapshot{
		alloc:        2400 * mib,
		retainedHeap: 2850 * mib,
	}

	if shouldWarnMemPressure(previous, current) {
		t.Fatal("normal retained heap warm-up below the pressure floor must not warn")
	}
}

func TestShouldWarnMemPressureSignalsActionableConditions(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)
	tests := []struct {
		name     string
		previous memPressureSnapshot
		current  memPressureSnapshot
	}{
		{
			name:    "absolute allocation",
			current: memPressureSnapshot{alloc: 6 * gib},
		},
		{
			name:    "host PSI",
			current: memPressureSnapshot{psiSome10: 1.0},
		},
		{
			name:     "retained heap growth",
			previous: memPressureSnapshot{retainedHeap: 2 * gib},
			current:  memPressureSnapshot{retainedHeap: 5 * gib},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !shouldWarnMemPressure(tt.previous, tt.current) {
				t.Fatal("expected memory pressure warning")
			}
		})
	}
}

func TestRetainedHeapBytesSubtractsMemoryReturnedToOS(t *testing.T) {
	const mib = uint64(1024 * 1024)
	if got, want := retainedHeapBytes(1500*mib, 400*mib), uint64(1100*mib); got != want {
		t.Fatalf("retained heap = %d, want %d", got, want)
	}
	if got := retainedHeapBytes(100*mib, 100*mib); got != 0 {
		t.Fatalf("retained heap must not underflow: %d", got)
	}
}
