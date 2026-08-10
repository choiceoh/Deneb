package server

import (
	"testing"
	"time"
)

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

func TestMemPressureWarningStateSuppressesUnchangedEpisode(t *testing.T) {
	var state memPressureWarningState
	start := time.Unix(1000, 0)

	if !state.shouldWarn(start, []string{"host_psi"}) {
		t.Fatal("first pressure tick must warn")
	}
	if state.shouldWarn(start.Add(30*time.Second), []string{"host_psi"}) {
		t.Fatal("unchanged pressure reason must not warn every tick")
	}
	if !state.shouldWarn(start.Add(time.Minute), []string{"host_psi", "heap_alloc"}) {
		t.Fatal("new pressure reason set must warn")
	}
	if state.shouldWarn(start.Add(2*time.Minute), nil) {
		t.Fatal("pressure clear must not warn")
	}
	if !state.shouldWarn(start.Add(3*time.Minute), []string{"host_psi"}) {
		t.Fatal("new pressure episode must warn")
	}
}

func TestMemPressureWarningStateRepeatsAfterCooldown(t *testing.T) {
	var state memPressureWarningState
	start := time.Unix(1000, 0)

	if !state.shouldWarn(start, []string{"host_psi"}) {
		t.Fatal("first pressure tick must warn")
	}
	if state.shouldWarn(start.Add(memPressureRepeatWarnEvery-time.Second), []string{"host_psi"}) {
		t.Fatal("pressure reminder must wait for cooldown")
	}
	if !state.shouldWarn(start.Add(memPressureRepeatWarnEvery), []string{"host_psi"}) {
		t.Fatal("persistent pressure must warn again after cooldown")
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
