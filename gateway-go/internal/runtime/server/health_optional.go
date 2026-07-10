package server

import "context"

// optionalHealthSections contains the independently collected /health sections
// that gracefully disappear when their backing runtime is unavailable.
type optionalHealthSections struct {
	cache        cacheHealthSection
	cachePresent bool
	gpu          []gpuStat
	gpuPresent   bool
}

type (
	cacheHealthProbe func(context.Context) (cacheHealthSection, bool)
	gpuHealthProbe   func(context.Context) ([]gpuStat, bool)
)

// collectOptionalHealth wires the production cache and GPU probes into the
// small concurrent collector. Both probes retain their own two-second timeout
// and receive the request context so an abandoned health request cancels work.
func (s *Server) collectOptionalHealth(ctx context.Context) optionalHealthSections {
	return collectOptionalHealthProbes(
		ctx,
		func(probeCtx context.Context) (cacheHealthSection, bool) {
			return s.cacheHealth.observe(probeCtx, s.vllmBaseURLs())
		},
		func(probeCtx context.Context) ([]gpuStat, bool) {
			return s.gpuHealth.observe(probeCtx, nil)
		},
	)
}

// collectOptionalHealthProbes runs independent optional probes concurrently and
// joins both before the caller renders JSON. A buffered result channel keeps
// each worker's completion independent from scheduler ordering.
func collectOptionalHealthProbes(
	ctx context.Context,
	cacheProbe cacheHealthProbe,
	gpuProbe gpuHealthProbe,
) optionalHealthSections {
	type probeResult struct {
		cache        bool
		cacheSection cacheHealthSection
		gpuStats     []gpuStat
		present      bool
		panicValue   any
	}

	results := make(chan probeResult, 2)
	go func() {
		result := probeResult{cache: true}
		defer func() {
			result.panicValue = recover()
			results <- result
		}()
		result.cacheSection, result.present = cacheProbe(ctx)
	}()
	go func() {
		result := probeResult{}
		defer func() {
			result.panicValue = recover()
			results <- result
		}()
		result.gpuStats, result.present = gpuProbe(ctx)
	}()

	var sections optionalHealthSections
	var probePanic any
	for range 2 {
		result := <-results
		if result.panicValue != nil {
			if probePanic == nil {
				probePanic = result.panicValue
			}
			continue
		}
		if result.cache {
			sections.cache = result.cacheSection
			sections.cachePresent = result.present
			continue
		}
		sections.gpu = result.gpuStats
		sections.gpuPresent = result.present
	}
	// A child goroutine sits outside net/http's handler panic recovery. Move a
	// probe panic back onto the caller goroutine so concurrency does not turn an
	// ordinary request failure into a process-wide crash.
	if probePanic != nil {
		panic(probePanic)
	}
	return sections
}
