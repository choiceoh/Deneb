package dentime

import (
	"sync"
	"testing"
	"time"
)

// resetForTest is a helper: clear env, clear config, reset the cache so each
// sub-test starts from a clean slate.
func resetForTest(t *testing.T) {
	t.Helper()
	t.Setenv(envVar, "")
	SetConfigTimezone("")
	ResetCache()
}

func TestNow_EnvVarWins(t *testing.T) {
	resetForTest(t)
	t.Setenv(envVar, "Asia/Seoul")
	ResetCache()

	loc := Location()
	if got, want := loc.String(), "Asia/Seoul"; got != want {
		t.Fatalf("Location name = %q, want %q", got, want)
	}
	if got, want := Name(), "Asia/Seoul"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}

	now := Now()
	// KST is a fixed UTC+9 offset. Verify the zone-aware time reports +9h.
	_, offset := now.Zone()
	if offset != 9*3600 {
		t.Fatalf("expected KST offset 32400s, got %d", offset)
	}

	// The env-selected zone must not match the server-local offset unless
	// the server happens to run in KST. We can still sanity-check that the
	// Unix instant matches time.Now() within a small skew.
	if delta := time.Since(now); delta < -time.Second || delta > time.Second {
		t.Fatalf("Now() drifted too far from real time: %v", delta)
	}
}

func TestEnvVar_InvalidFallsBackToLocal(t *testing.T) {
	resetForTest(t)
	t.Setenv(envVar, "Invalid/NotAZone")
	ResetCache()

	// Must not panic, must not return nil, must fall through to local.
	loc := Location()
	if loc == nil {
		t.Fatal("Location() returned nil on invalid env")
	}
	if got, want := Name(), "Local"; got != want {
		t.Fatalf("Name() with invalid env = %q, want %q", got, want)
	}
}

func TestConfigPath_PicksUpAfterReset(t *testing.T) {
	resetForTest(t)
	SetConfigTimezone("America/Los_Angeles")

	loc := Location()
	if got, want := loc.String(), "America/Los_Angeles"; got != want {
		t.Fatalf("Location name = %q, want %q", got, want)
	}
	if got, want := Name(), "America/Los_Angeles"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	resetForTest(t)
	SetConfigTimezone("America/Los_Angeles")
	t.Setenv(envVar, "Asia/Seoul")
	ResetCache()

	if got, want := Name(), "Asia/Seoul"; got != want {
		t.Fatalf("Name() with env+config = %q, want %q", got, want)
	}
}

func TestWhitespaceTreatedAsEmpty(t *testing.T) {
	resetForTest(t)
	t.Setenv(envVar, "   ")
	SetConfigTimezone("  ")
	ResetCache()

	loc := Location()
	if loc == nil {
		t.Fatal("Location() returned nil with whitespace-only env/config")
	}
	if got, want := Name(), "Local"; got != want {
		t.Fatalf("Name() with whitespace = %q, want %q", got, want)
	}
}

func TestConfigInvalidFallsBackToLocal(t *testing.T) {
	resetForTest(t)
	SetConfigTimezone("Not/AZone")

	if got, want := Name(), "Local"; got != want {
		t.Fatalf("Name() with invalid config = %q, want %q", got, want)
	}
}

func TestSetConfigTimezone_ClearsCache(t *testing.T) {
	resetForTest(t)
	SetConfigTimezone("UTC")
	if got, want := Name(), "UTC"; got != want {
		t.Fatalf("initial Name() = %q, want %q", got, want)
	}

	// Overwriting must take effect on the next call — SetConfigTimezone
	// must reset the cache.
	SetConfigTimezone("Asia/Seoul")
	if got, want := Name(), "Asia/Seoul"; got != want {
		t.Fatalf("after overwrite Name() = %q, want %q", got, want)
	}

	// Clearing should fall back to Local.
	SetConfigTimezone("")
	if got, want := Name(), "Local"; got != want {
		t.Fatalf("after clear Name() = %q, want %q", got, want)
	}
}

func TestEmptyBothFallsBackToLocal(t *testing.T) {
	resetForTest(t)
	// Neither env nor config set → Local.
	if got, want := Name(), "Local"; got != want {
		t.Fatalf("Name() with neither source = %q, want %q", got, want)
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Exercise Now() under contention to catch races when run with -race.
	resetForTest(t)
	t.Setenv(envVar, "Asia/Seoul")
	ResetCache()

	var wg sync.WaitGroup
	const goroutines = 32
	const iterations = 200
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = Now()
				_ = Location()
				_ = Name()
			}
		}()
	}
	wg.Wait()

	if got, want := Name(), "Asia/Seoul"; got != want {
		t.Fatalf("after concurrent access Name() = %q, want %q", got, want)
	}
}

func TestConcurrentSetAndRead(t *testing.T) {
	// Writer thrash + reader thrash. -race must stay clean.
	resetForTest(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers.
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = Now()
					_ = Name()
				}
			}
		}()
	}

	// Writers flip between valid zones.
	zones := []string{"Asia/Seoul", "UTC", "America/Los_Angeles"}
	for i := range 4 {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for k := range 100 {
				SetConfigTimezone(zones[(start+k)%len(zones)])
			}
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestInvalidZone_WarnsOncePerName(t *testing.T) {
	// We can't easily assert slog output without redirecting, but we can at
	// least verify the public behavior stays stable across many invalid calls:
	// never panic, always fall back, name stays "Local".
	resetForTest(t)
	t.Setenv(envVar, "DEFINITELY/NotReal")
	ResetCache()

	for i := range 50 {
		if got, want := Name(), "Local"; got != want {
			t.Fatalf("iteration %d: Name() = %q, want %q", i, got, want)
		}
	}
}
