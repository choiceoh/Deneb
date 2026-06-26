package server

import (
	"context"
	"reflect"
	"testing"
)

func TestParseGPUStats(t *testing.T) {
	cases := []struct {
		desc string
		csv  string
		want []gpuStat
	}{
		{
			desc: "single GPU typical DGX Spark output",
			csv:  "37, 18432, 81920, 54\n",
			want: []gpuStat{{Index: 0, UtilPct: 37, MemUsedMiB: 18432, MemTotalMiB: 81920, TempC: 54}},
		},
		{
			desc: "multi-GPU rows index in order",
			csv:  "0, 1024, 81920, 41\n100, 80000, 81920, 73\n",
			want: []gpuStat{
				{Index: 0, UtilPct: 0, MemUsedMiB: 1024, MemTotalMiB: 81920, TempC: 41},
				{Index: 1, UtilPct: 100, MemUsedMiB: 80000, MemTotalMiB: 81920, TempC: 73},
			},
		},
		{
			desc: "no trailing newline",
			csv:  "12, 2048, 81920, 48",
			want: []gpuStat{{Index: 0, UtilPct: 12, MemUsedMiB: 2048, MemTotalMiB: 81920, TempC: 48}},
		},
		{
			desc: "N/A field tolerated as zero (temp unsupported)",
			csv:  "5, 512, 81920, [N/A]\n",
			want: []gpuStat{{Index: 0, UtilPct: 5, MemUsedMiB: 512, MemTotalMiB: 81920, TempC: 0}},
		},
		{
			desc: "stray unit suffix stripped (defensive, nounits should prevent)",
			csv:  "37 %, 18432 MiB, 81920 MiB, 54 C\n",
			want: []gpuStat{{Index: 0, UtilPct: 37, MemUsedMiB: 18432, MemTotalMiB: 81920, TempC: 54}},
		},
		{
			desc: "blank lines skipped, malformed short row skipped, good rows kept",
			csv:  "\n10, 100, 200, 30\nbroken,row\n20, 200, 400, 40\n\n",
			want: []gpuStat{
				{Index: 0, UtilPct: 10, MemUsedMiB: 100, MemTotalMiB: 200, TempC: 30},
				{Index: 1, UtilPct: 20, MemUsedMiB: 200, MemTotalMiB: 400, TempC: 40},
			},
		},
		{
			desc: "empty input yields no rows",
			csv:  "",
			want: nil,
		},
		{
			desc: "whitespace-only input yields no rows",
			csv:  "   \n\t\n",
			want: nil,
		},
		{
			desc: "garbage numeric cells default to zero, row still emitted",
			csv:  "abc, def, 81920, 50\n",
			want: []gpuStat{{Index: 0, UtilPct: 0, MemUsedMiB: 0, MemTotalMiB: 81920, TempC: 50}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := parseGPUStats(tc.csv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseGPUStats(%q) = %+v, want %+v", tc.csv, got, tc.want)
			}
		})
	}
}

func TestParseGPUInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"37", 37},
		{" 37 ", 37},
		{"0", 0},
		{"", 0},
		{"[N/A]", 0},
		{"[Not Supported]", 0},
		{"81920 MiB", 81920},
		{"54 C", 54},
		{"abc", 0},
		{"12.5", 12}, // decimal point is non-digit → truncates at it
	}
	for _, tc := range cases {
		if got := parseGPUInt(tc.in); got != tc.want {
			t.Errorf("parseGPUInt(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestGPUHealthObserveAbsentDegrades verifies the core graceful-degradation
// contract: when nvidia-smi is absent (runner reports ok=false), observe
// reports present=false and no stats, so /health omits the gpu section instead
// of erroring on a non-GPU host.
func TestGPUHealthObserveAbsentDegrades(t *testing.T) {
	var g gpuHealth
	absent := func(context.Context) (string, bool) { return "", false }
	stats, present := g.observe(context.Background(), absent)
	if present {
		t.Errorf("present = true on absent nvidia-smi, want false")
	}
	if stats != nil {
		t.Errorf("stats = %+v on absent nvidia-smi, want nil", stats)
	}
}

// TestGPUHealthObservePresentAndCached verifies a present GPU is parsed and
// then served from cache without re-invoking the runner within the TTL.
func TestGPUHealthObservePresentAndCached(t *testing.T) {
	var g gpuHealth
	calls := 0
	runner := func(context.Context) (string, bool) {
		calls++
		return "50, 4096, 81920, 60\n", true
	}
	stats, present := g.observe(context.Background(), runner)
	if !present || len(stats) != 1 || stats[0].UtilPct != 50 || stats[0].MemUsedMiB != 4096 {
		t.Fatalf("first observe = (%+v, %v), want one GPU at 50%%/4096MiB present", stats, present)
	}
	// Second call within TTL must be served from cache (runner not re-invoked).
	if _, _ = g.observe(context.Background(), runner); calls != 1 {
		t.Errorf("runner called %d times, want 1 (cached within TTL)", calls)
	}
}

// TestGPUHealthObserveOKButEmptyTreatedAbsent guards the edge where the binary
// runs but yields no parseable rows — we must not render an empty gpu section.
func TestGPUHealthObserveOKButEmptyTreatedAbsent(t *testing.T) {
	var g gpuHealth
	runner := func(context.Context) (string, bool) { return "\n  \n", true }
	stats, present := g.observe(context.Background(), runner)
	if present || stats != nil {
		t.Errorf("observe = (%+v, %v) on ok-but-empty output, want (nil, false)", stats, present)
	}
}
