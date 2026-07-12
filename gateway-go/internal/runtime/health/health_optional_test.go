package health

import (
	"context"
	"testing"
	"time"
)

func TestCollectOptionalHealthProbesRunConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})

	wantCache := CacheSection{WindowQueries: 11, WindowHits: 7, Samples: 2}
	wantGPU := []GPUStat{{Index: 0, UtilPct: 42}}
	done := make(chan optionalHealthSections, 1)
	go func() {
		done <- collectOptionalHealthProbes(
			context.Background(),
			func(context.Context) (CacheSection, bool) {
				started <- "cache"
				<-release
				return wantCache, true
			},
			func(context.Context) ([]GPUStat, bool) {
				started <- "gpu"
				<-release
				return wantGPU, true
			},
		)
	}()

	seen := waitForHealthProbes(t, started)
	if !seen["cache"] || !seen["gpu"] {
		t.Fatalf("probes did not both start before release: %v", seen)
	}
	close(release)
	released = true

	select {
	case got := <-done:
		if !got.cachePresent || got.cache.WindowQueries != wantCache.WindowQueries || got.cache.WindowHits != wantCache.WindowHits {
			t.Fatalf("unexpected cache section: %+v", got)
		}
		if !got.gpuPresent || len(got.gpu) != 1 || got.gpu[0] != wantGPU[0] {
			t.Fatalf("unexpected GPU section: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("optional health collection did not finish after release")
	}
}

func TestCollectOptionalHealthProbesPropagateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 2)
	canceled := make(chan string, 2)
	done := make(chan optionalHealthSections, 1)

	probe := func(name string) func(context.Context) bool {
		return func(probeCtx context.Context) bool {
			started <- name
			<-probeCtx.Done()
			if probeCtx.Err() == context.Canceled {
				canceled <- name
			}
			return false
		}
	}
	cacheProbe := probe("cache")
	gpuProbe := probe("gpu")
	go func() {
		done <- collectOptionalHealthProbes(
			ctx,
			func(probeCtx context.Context) (CacheSection, bool) {
				return CacheSection{}, cacheProbe(probeCtx)
			},
			func(probeCtx context.Context) ([]GPUStat, bool) {
				return nil, gpuProbe(probeCtx)
			},
		)
	}()

	waitForHealthProbes(t, started)
	cancel()
	seenCanceled := waitForHealthProbes(t, canceled)
	if !seenCanceled["cache"] || !seenCanceled["gpu"] {
		t.Fatalf("request cancellation did not reach both probes: %v", seenCanceled)
	}

	select {
	case got := <-done:
		if got.cachePresent || got.gpuPresent {
			t.Fatalf("canceled probes must remain absent: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("optional health collection did not finish after cancellation")
	}
}

func TestCollectOptionalHealthProbesRepanicsOnCaller(t *testing.T) {
	defer func() {
		if got := recover(); got != "cache probe panic" {
			t.Fatalf("recovered panic = %v, want cache probe panic", got)
		}
	}()

	collectOptionalHealthProbes(
		context.Background(),
		func(context.Context) (CacheSection, bool) {
			panic("cache probe panic")
		},
		func(context.Context) ([]GPUStat, bool) {
			return nil, false
		},
	)
	t.Fatal("expected collector to propagate probe panic")
}

func waitForHealthProbes(t *testing.T, started <-chan string) map[string]bool {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	seen := make(map[string]bool, 2)
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-timer.C:
			t.Fatalf("timed out waiting for both probes; saw %v", seen)
		}
	}
	return seen
}
