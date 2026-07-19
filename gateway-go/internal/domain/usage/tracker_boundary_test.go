package usage

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestNewTrackerBoundaryState(t *testing.T) {
	t.Parallel()

	before := time.Now()
	tracker := New()
	after := time.Now()
	if tracker == nil {
		t.Fatal("New returned nil")
	}
	if tracker.providers == nil {
		t.Fatal("New left providers nil")
	}
	if len(tracker.providers) != 0 {
		t.Fatalf("new providers = %#v, want empty", tracker.providers)
	}
	if tracker.startedAt.Before(before) || tracker.startedAt.After(after) {
		t.Fatalf("startedAt %v outside [%v,%v]", tracker.startedAt, before, after)
	}
	status := tracker.Status()
	if status == nil || status.Providers == nil {
		t.Fatalf("Status = %#v", status)
	}
	if len(status.Providers) != 0 {
		t.Fatalf("Status providers = %#v", status.Providers)
	}
	cost := tracker.Cost()
	if cost == nil || cost.Providers == nil || cost.TotalCalls != 0 {
		t.Fatalf("Cost = %#v", cost)
	}
}

func TestRecordCallPreservesProviderKeyExactly(t *testing.T) {
	t.Parallel()

	providers := []string{
		"",
		" ",
		"openai",
		"OpenAI",
		" openai ",
		"anthropic/claude",
		"provider:region:model",
		"한글 제공자",
		"line\nbreak",
		"tab\tkey",
	}
	tracker := New()
	for repeat := 0; repeat < 3; repeat++ {
		for _, provider := range providers {
			tracker.RecordCall(provider)
		}
	}
	status := tracker.Status()
	if len(status.Providers) != len(providers) {
		t.Fatalf("provider count = %d, want %d: %#v", len(status.Providers), len(providers), status.Providers)
	}
	for _, provider := range providers {
		stats, ok := status.Providers[provider]
		if !ok {
			t.Errorf("provider key %q missing", provider)
			continue
		}
		if stats.Calls != 3 {
			t.Errorf("provider %q calls = %d, want 3", provider, stats.Calls)
		}
	}
}

func TestRecordTokensSignedBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		increments []TokenUsage
		want       TokenUsage
	}{
		{
			name:       "all zero",
			increments: []TokenUsage{{}},
			want:       TokenUsage{},
		},
		{
			name: "single positive",
			increments: []TokenUsage{{
				Input:      1,
				Output:     2,
				CacheRead:  3,
				CacheWrite: 4,
			}},
			want: TokenUsage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
		},
		{
			name: "independent fields",
			increments: []TokenUsage{
				{Input: 10},
				{Output: 20},
				{CacheRead: 30},
				{CacheWrite: 40},
			},
			want: TokenUsage{Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40},
		},
		{
			name: "positive accumulation",
			increments: []TokenUsage{
				{Input: 100, Output: 200, CacheRead: 300, CacheWrite: 400},
				{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
				{Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40},
			},
			want: TokenUsage{Input: 111, Output: 222, CacheRead: 333, CacheWrite: 444},
		},
		{
			name: "signed corrections",
			increments: []TokenUsage{
				{Input: 100, Output: 200, CacheRead: 300, CacheWrite: 400},
				{Input: -10, Output: -20, CacheRead: -30, CacheWrite: -40},
			},
			want: TokenUsage{Input: 90, Output: 180, CacheRead: 270, CacheWrite: 360},
		},
		{
			name: "net zero",
			increments: []TokenUsage{
				{Input: 9, Output: 8, CacheRead: 7, CacheWrite: 6},
				{Input: -9, Output: -8, CacheRead: -7, CacheWrite: -6},
			},
			want: TokenUsage{},
		},
		{
			name: "large representable",
			increments: []TokenUsage{
				{Input: math.MaxInt32, Output: math.MaxInt32, CacheRead: math.MaxInt32, CacheWrite: math.MaxInt32},
				{Input: math.MaxInt32, Output: math.MaxInt32, CacheRead: math.MaxInt32, CacheWrite: math.MaxInt32},
			},
			want: TokenUsage{
				Input:      2 * math.MaxInt32,
				Output:     2 * math.MaxInt32,
				CacheRead:  2 * math.MaxInt32,
				CacheWrite: 2 * math.MaxInt32,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tracker := New()
			for _, inc := range tc.increments {
				tracker.RecordTokens("provider", inc.Input, inc.Output, inc.CacheRead, inc.CacheWrite)
			}
			got := tracker.Status().Providers["provider"]
			if got == nil {
				t.Fatal("provider missing")
			}
			if got.Tokens != tc.want {
				t.Fatalf("tokens = %#v, want %#v", got.Tokens, tc.want)
			}
			if got.Calls != 0 {
				t.Fatalf("RecordTokens changed calls to %d", got.Calls)
			}
		})
	}
}

func TestRecordCallAndRecordTokensPreserveEachOthersFields(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.RecordCall("provider")
	tracker.RecordCall("provider")
	beforeTokens := tracker.Status().Providers["provider"].Tokens
	tracker.RecordTokens("provider", 11, 22, 33, 44)
	afterTokens := tracker.Status().Providers["provider"]
	if afterTokens.Calls != 2 {
		t.Fatalf("RecordTokens changed Calls = %d", afterTokens.Calls)
	}
	if afterTokens.Tokens != (TokenUsage{Input: 11, Output: 22, CacheRead: 33, CacheWrite: 44}) {
		t.Fatalf("Tokens = %#v", afterTokens.Tokens)
	}
	tracker.RecordCall("provider")
	afterCall := tracker.Status().Providers["provider"]
	if afterCall.Calls != 3 {
		t.Fatalf("Calls = %d", afterCall.Calls)
	}
	if afterCall.Tokens != afterTokens.Tokens || beforeTokens != (TokenUsage{}) {
		t.Fatalf("RecordCall changed tokens: before=%#v tokenSnapshot=%#v after=%#v", beforeTokens, afterTokens.Tokens, afterCall.Tokens)
	}
}

func TestRecordOperationsCreateProviderOnFirstUse(t *testing.T) {
	t.Parallel()

	t.Run("call", func(t *testing.T) {
		t.Parallel()
		tracker := New()
		tracker.RecordCall("new")
		got := tracker.Status().Providers["new"]
		if got == nil || got.Calls != 1 || got.Tokens != (TokenUsage{}) {
			t.Fatalf("stats = %#v", got)
		}
	})
	t.Run("tokens", func(t *testing.T) {
		t.Parallel()
		tracker := New()
		tracker.RecordTokens("new", 1, 2, 3, 4)
		got := tracker.Status().Providers["new"]
		if got == nil || got.Calls != 0 || got.Tokens != (TokenUsage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}) {
			t.Fatalf("stats = %#v", got)
		}
	})
}

func TestStatusTimestampAndUptimeFormatting(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.startedAt = time.Now().Add(-3661*time.Second - 900*time.Millisecond)
	status := tracker.Status()
	parsed, err := time.Parse(time.RFC3339, status.StartedAt)
	if err != nil {
		t.Fatalf("StartedAt %q is not RFC3339: %v", status.StartedAt, err)
	}
	if !parsed.Equal(tracker.startedAt.Truncate(time.Second)) {
		t.Fatalf("parsed StartedAt = %v, want %v", parsed, tracker.startedAt.Truncate(time.Second))
	}
	if status.Uptime != "1h1m1s" && status.Uptime != "1h1m2s" {
		t.Fatalf("Uptime = %q, want near 1h1m1s", status.Uptime)
	}
}

func TestStatusUptimeTruncatesSubsecondDuration(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.startedAt = time.Now().Add(-450 * time.Millisecond)
	if got := tracker.Status().Uptime; got != "0s" {
		t.Fatalf("Uptime = %q, want 0s", got)
	}
}

func TestStatusReturnsIndependentSnapshotsImmuneToMutation(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.RecordCall("alpha")
	tracker.RecordTokens("alpha", 1, 2, 3, 4)
	tracker.RecordCall("beta")
	one := tracker.Status()
	two := tracker.Status()

	one.Providers["alpha"].Calls = 999
	one.Providers["alpha"].Tokens.Input = 999
	delete(one.Providers, "beta")
	one.Providers["injected"] = &ProviderStats{Calls: 777}
	one.Uptime = "changed"
	one.StartedAt = "changed"

	if two.Providers["alpha"].Calls != 1 || two.Providers["alpha"].Tokens.Input != 1 {
		t.Fatalf("second snapshot shares values: %#v", two.Providers["alpha"])
	}
	if two.Providers["beta"] == nil || two.Providers["injected"] != nil {
		t.Fatalf("second snapshot shares map: %#v", two.Providers)
	}
	three := tracker.Status()
	if three.Providers["alpha"].Calls != 1 || three.Providers["alpha"].Tokens.Input != 1 || three.Providers["beta"] == nil || three.Providers["injected"] != nil {
		t.Fatalf("tracker mutated through Status snapshot: %#v", three)
	}
}

func TestCostReturnsIndependentSnapshotsImmuneToMutation(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.RecordCall("alpha")
	tracker.RecordTokens("alpha", 10, 20, 30, 40)
	tracker.RecordCall("beta")
	one := tracker.Cost()
	two := tracker.Cost()

	one.TotalCalls = 999
	one.Providers["alpha"].Calls = 999
	one.Providers["alpha"].Tokens.CacheRead = 999
	delete(one.Providers, "beta")
	one.Providers["injected"] = &ProviderStats{Calls: 777}

	if two.TotalCalls != 2 {
		t.Fatalf("second TotalCalls = %d", two.TotalCalls)
	}
	if two.Providers["alpha"].Calls != 1 || two.Providers["alpha"].Tokens.CacheRead != 30 {
		t.Fatalf("second snapshot shares values: %#v", two.Providers["alpha"])
	}
	if two.Providers["beta"] == nil || two.Providers["injected"] != nil {
		t.Fatalf("second snapshot shares map: %#v", two.Providers)
	}
	three := tracker.Cost()
	if three.TotalCalls != 2 || three.Providers["alpha"].Calls != 1 || three.Providers["beta"] == nil {
		t.Fatalf("tracker mutated through Cost snapshot: %#v", three)
	}
}

func TestStatusAndCostReturnIndependentSnapshots(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.RecordCall("provider")
	tracker.RecordTokens("provider", 1, 2, 3, 4)
	status := tracker.Status()
	cost := tracker.Cost()
	status.Providers["provider"].Calls = 100
	status.Providers["provider"].Tokens.Output = 100
	if cost.Providers["provider"].Calls != 1 || cost.Providers["provider"].Tokens.Output != 2 {
		t.Fatalf("Cost shares Status values: %#v", cost.Providers["provider"])
	}
	cost.Providers["provider"].Calls = 200
	cost.Providers["provider"].Tokens.Output = 200
	if status.Providers["provider"].Calls != 100 || status.Providers["provider"].Tokens.Output != 100 {
		t.Fatalf("Status shares Cost values: %#v", status.Providers["provider"])
	}
}

func TestCostTotalCallsSumsSignedCountsAtNegativeBoundary(t *testing.T) {
	t.Parallel()

	tracker := New()
	tracker.mu.Lock()
	tracker.providers["positive"] = &ProviderStats{Calls: 15}
	tracker.providers["negative"] = &ProviderStats{Calls: -4}
	tracker.providers["zero"] = &ProviderStats{Calls: 0}
	tracker.mu.Unlock()
	cost := tracker.Cost()
	if cost.TotalCalls != 11 {
		t.Fatalf("TotalCalls = %d, want signed sum 11", cost.TotalCalls)
	}
}

func TestProviderStatsJSONTagsMatchExpectedFieldFormat(t *testing.T) {
	t.Parallel()

	usageType := reflect.TypeOf(TokenUsage{})
	wantUsage := map[string]string{
		"Input":      "input",
		"Output":     "output",
		"CacheRead":  "cacheRead",
		"CacheWrite": "cacheWrite",
	}
	for fieldName, jsonName := range wantUsage {
		field, ok := usageType.FieldByName(fieldName)
		if !ok || field.Tag.Get("json") != jsonName {
			t.Errorf("TokenUsage.%s json tag = %q, want %q", fieldName, field.Tag.Get("json"), jsonName)
		}
	}
	statsType := reflect.TypeOf(ProviderStats{})
	for fieldName, jsonName := range map[string]string{"Calls": "calls", "Tokens": "tokens"} {
		field, ok := statsType.FieldByName(fieldName)
		if !ok || field.Tag.Get("json") != jsonName {
			t.Errorf("ProviderStats.%s json tag = %q, want %q", fieldName, field.Tag.Get("json"), jsonName)
		}
	}
}

func TestConcurrentRecordCallsExactTotals(t *testing.T) {
	const (
		workers    = 64
		iterations = 1000
		providers  = 11
	)
	tracker := New()
	start := make(chan struct{})
	var wg sync.WaitGroup
	want := make([]int64, providers)
	for worker := 0; worker < workers; worker++ {
		for iteration := 0; iteration < iterations; iteration++ {
			want[(worker+iteration)%providers]++
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				provider := fmt.Sprintf("provider-%02d", (worker+iteration)%providers)
				tracker.RecordCall(provider)
			}
		}()
	}
	close(start)
	wg.Wait()
	cost := tracker.Cost()
	if cost.TotalCalls != workers*iterations {
		t.Fatalf("TotalCalls = %d, want %d", cost.TotalCalls, workers*iterations)
	}
	for provider := 0; provider < providers; provider++ {
		name := fmt.Sprintf("provider-%02d", provider)
		if got := cost.Providers[name].Calls; got != want[provider] {
			t.Errorf("%s calls = %d, want %d", name, got, want[provider])
		}
	}
}

func TestConcurrentRecordTokensExactTotals(t *testing.T) {
	const (
		workers    = 48
		iterations = 750
	)
	tracker := New()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				tracker.RecordTokens("shared", 1, 2, 3, 4)
			}
		}()
	}
	close(start)
	wg.Wait()
	got := tracker.Status().Providers["shared"]
	writes := int64(workers * iterations)
	want := TokenUsage{
		Input:      writes,
		Output:     2 * writes,
		CacheRead:  3 * writes,
		CacheWrite: 4 * writes,
	}
	if got.Tokens != want || got.Calls != 0 {
		t.Fatalf("stats = %#v, want tokens %#v and zero calls", got, want)
	}
}

func TestConcurrentMixedRecordAndSnapshotOperations(t *testing.T) {
	const (
		writers    = 32
		readers    = 16
		iterations = 500
	)
	tracker := New()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			provider := fmt.Sprintf("provider-%d", writer%4)
			for i := 0; i < iterations; i++ {
				tracker.RecordCall(provider)
				tracker.RecordTokens(provider, 1, 2, 3, 4)
			}
		}()
	}
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				status := tracker.Status()
				cost := tracker.Cost()
				var summed int64
				for _, stats := range cost.Providers {
					if stats.Calls < 0 || stats.Tokens.Input < 0 || stats.Tokens.Output < 0 || stats.Tokens.CacheRead < 0 || stats.Tokens.CacheWrite < 0 {
						t.Errorf("negative torn stats: %#v", stats)
						return
					}
					summed += stats.Calls
				}
				if summed != cost.TotalCalls {
					t.Errorf("Cost TotalCalls=%d, sum=%d", cost.TotalCalls, summed)
					return
				}
				for _, stats := range status.Providers {
					if stats.Tokens.Output != 2*stats.Tokens.Input || stats.Tokens.CacheRead != 3*stats.Tokens.Input || stats.Tokens.CacheWrite != 4*stats.Tokens.Input {
						t.Errorf("torn Status stats: %#v", stats)
						return
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	cost := tracker.Cost()
	if cost.TotalCalls != writers*iterations {
		t.Fatalf("final TotalCalls = %d, want %d", cost.TotalCalls, writers*iterations)
	}
	for name, stats := range cost.Providers {
		if stats.Calls != stats.Tokens.Input {
			t.Errorf("%s calls=%d input=%d", name, stats.Calls, stats.Tokens.Input)
		}
	}
}

func TestProviderKeysRemainDistinctUnderConcurrency(t *testing.T) {
	keys := []string{"a", "A", " a", "a ", "a\n", "á", "한글", ""}
	tracker := New()
	const iterations = 1000
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tracker.RecordCall(key)
			}
		}()
	}
	wg.Wait()
	status := tracker.Status()
	if len(status.Providers) != len(keys) {
		t.Fatalf("provider keys collapsed: %#v", status.Providers)
	}
	for _, key := range keys {
		if status.Providers[key] == nil || status.Providers[key].Calls != iterations {
			t.Errorf("key %q stats = %#v", key, status.Providers[key])
		}
	}
}

func TestCostAndStatusReturnMatchingProviderSets(t *testing.T) {
	t.Parallel()

	tracker := New()
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("provider-%02d", i)
		if i%2 == 0 {
			tracker.RecordCall(name)
		} else {
			tracker.RecordTokens(name, int64(i), int64(i*2), int64(i*3), int64(i*4))
		}
	}
	status := tracker.Status()
	cost := tracker.Cost()
	statusKeys := sortedProviderKeys(status.Providers)
	costKeys := sortedProviderKeys(cost.Providers)
	if !reflect.DeepEqual(statusKeys, costKeys) {
		t.Fatalf("Status keys=%v Cost keys=%v", statusKeys, costKeys)
	}
	for _, key := range statusKeys {
		if *status.Providers[key] != *cost.Providers[key] {
			t.Errorf("%s differs: status=%#v cost=%#v", key, status.Providers[key], cost.Providers[key])
		}
	}
}

func sortedProviderKeys(providers map[string]*ProviderStats) []string {
	keys := make([]string, 0, len(providers))
	for key := range providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
