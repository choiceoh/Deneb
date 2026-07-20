package typing

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

func waitFor(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestNewTypingControllerUsesOverridesAndDefaultsForInvalidValues(t *testing.T) {
	defaults := NewTypingController(TypingControllerConfig{})
	defer defaults.Cleanup()
	if defaults.intervalMs != 6000 || defaults.ttlMs != 30000 || defaults.silentToken != tokens.SilentReplyToken {
		t.Fatalf("defaults = interval=%d ttl=%d token=%q", defaults.intervalMs, defaults.ttlMs, defaults.silentToken)
	}
	custom := NewTypingController(TypingControllerConfig{IntervalMs: 17, TtlMs: 23, SilentToken: "SILENT"})
	defer custom.Cleanup()
	if custom.intervalMs != 17 || custom.ttlMs != 23 || custom.silentToken != "SILENT" {
		t.Fatalf("custom = interval=%d ttl=%d token=%q", custom.intervalMs, custom.ttlMs, custom.silentToken)
	}
	negative := NewTypingController(TypingControllerConfig{IntervalMs: -1, TtlMs: -1})
	defer negative.Cleanup()
	if negative.intervalMs != 6000 || negative.ttlMs != 30000 {
		t.Fatalf("negative config did not default: %+v", negative)
	}
}

func TestTypingControllerStartKeepaliveStopAndIdempotence(t *testing.T) {
	var starts, stops atomic.Int32
	controller := NewTypingController(TypingControllerConfig{
		IntervalMs: 5,
		TtlMs:      100,
		OnStart:    func() { starts.Add(1) },
		OnStop:     func() { stops.Add(1) },
	})
	controller.Start()
	controller.StartTypingLoop()
	if !controller.IsStarted() || !controller.IsActive() {
		t.Fatal("controller did not start")
	}
	waitFor(t, 100*time.Millisecond, func() bool { return starts.Load() >= 2 }, "keepalive callback")
	controller.Stop()
	controller.Stop()
	controller.Start()
	if controller.IsActive() || starts.Load() < 2 || stops.Load() != 1 {
		t.Fatalf("stop state starts=%d stops=%d active=%v", starts.Load(), stops.Load(), controller.IsActive())
	}
}

func TestTypingControllerTTLExpiryCanRestart(t *testing.T) {
	var starts atomic.Int32
	controller := NewTypingController(TypingControllerConfig{
		IntervalMs: 3,
		TtlMs:      12,
		OnStart:    func() { starts.Add(1) },
	})
	defer controller.Cleanup()
	controller.Start()
	waitFor(t, 100*time.Millisecond, func() bool { return !controller.IsActive() }, "TTL expiry")
	if controller.IsStarted() {
		t.Fatal("expired controller remained marked started")
	}
	firstStarts := starts.Load()
	controller.StartTypingLoop()
	if !controller.IsActive() || !controller.IsStarted() {
		t.Fatal("expired controller did not restart")
	}
	waitFor(t, 100*time.Millisecond, func() bool { return starts.Load() > firstStarts }, "restart callback")
}

// fakeClock is a mutex-guarded manual clock for pinning TypingController TTL
// math, so TTL assertions never depend on real scheduler timing.
type fakeClock struct {
	mu  sync.Mutex
	cur time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.cur = c.cur.Add(d)
	c.mu.Unlock()
}

func TestRefreshTypingTTLUpdatesActiveDeadline(t *testing.T) {
	var ticks atomic.Int32
	clock := &fakeClock{cur: time.Unix(0, 0)}
	controller := NewTypingController(TypingControllerConfig{
		IntervalMs: 2,
		TtlMs:      15,
		OnStart:    func() { ticks.Add(1) },
	})
	controller.now = clock.Now // before Start: keepalive goroutine not yet running
	defer controller.Cleanup()
	controller.Start() // deadline = 15ms on the fake clock

	clock.Advance(10 * time.Millisecond)
	controller.RefreshTypingTTL() // deadline = 10ms + 15ms = 25ms
	// Past the original 15ms deadline but inside the refreshed one.
	clock.Advance(9 * time.Millisecond)

	// Let the keepalive loop evaluate the TTL a couple of times at the frozen
	// 19ms clock; with the refreshed deadline it must survive every check.
	base := ticks.Load()
	waitFor(t, time.Second, func() bool { return ticks.Load() >= base+2 }, "keepalive ticks after refresh")
	if !controller.IsActive() {
		t.Fatal("controller expired before refreshed deadline")
	}

	clock.Advance(10 * time.Millisecond) // 29ms > refreshed 25ms deadline
	waitFor(t, time.Second, func() bool { return !controller.IsActive() }, "refreshed TTL expiry")
}

func TestStartTypingOnTextSilentAndCustomTokens(t *testing.T) {
	var starts atomic.Int32
	controller := NewTypingController(TypingControllerConfig{
		SilentToken: "SILENT_REPLY",
		OnStart:     func() { starts.Add(1) },
	})
	defer controller.Cleanup()
	for _, text := range []string{"", "  ", "SILENT_", "SILENT_REPLY", " SILENT_REPLY "} {
		controller.StartTypingOnText(text)
	}
	if starts.Load() != 0 || controller.IsStarted() {
		t.Fatalf("silent prefixes started typing: starts=%d", starts.Load())
	}
	controller.StartTypingOnText("visible")
	if starts.Load() != 1 || !controller.IsActive() {
		t.Fatalf("visible text did not start: starts=%d active=%v", starts.Load(), controller.IsActive())
	}
	(*TypingController)(nil).StartTypingOnText("visible")
}

func TestCleanupStopsIdempotentlyAndSealIgnoresFurtherStop(t *testing.T) {
	var stops, cleanups atomic.Int32
	controller := NewTypingController(TypingControllerConfig{
		OnStop:    func() { stops.Add(1) },
		OnCleanup: func() { cleanups.Add(1) },
	})
	controller.Start()
	controller.Cleanup()
	controller.Cleanup()
	controller.Stop()
	if stops.Load() != 1 || cleanups.Load() != 1 {
		t.Fatalf("cleanup callbacks stops=%d cleanups=%d", stops.Load(), cleanups.Load())
	}

	var sealedStops atomic.Int32
	sealed := NewTypingController(TypingControllerConfig{OnStop: func() { sealedStops.Add(1) }})
	sealed.Start()
	sealed.Seal()
	sealed.Stop()
	if sealed.IsActive() || sealedStops.Load() != 0 {
		t.Fatalf("Seal invoked stop callback or remained active: %d", sealedStops.Load())
	}
}

func TestMarkRunCompleteAndDispatchIdleStopIdempotentlyInEitherOrder(t *testing.T) {
	for _, order := range []string{"run-first", "dispatch-first"} {
		t.Run(order, func(t *testing.T) {
			var stops atomic.Int32
			controller := NewTypingController(TypingControllerConfig{OnStop: func() { stops.Add(1) }})
			controller.Start()
			if order == "run-first" {
				controller.MarkRunComplete()
				if !controller.IsActive() {
					t.Fatal("run completion stopped before dispatch became idle")
				}
				controller.MarkDispatchIdle()
			} else {
				controller.MarkDispatchIdle()
				if !controller.IsActive() {
					t.Fatal("dispatch idle stopped before run completed")
				}
				controller.MarkRunComplete()
			}
			if controller.IsActive() || stops.Load() != 1 {
				t.Fatalf("combined completion active=%v stops=%d", controller.IsActive(), stops.Load())
			}
			controller.MarkRunComplete()
			controller.MarkDispatchIdle()
			if stops.Load() != 1 {
				t.Fatalf("duplicate completion called stop %d times", stops.Load())
			}
		})
	}
}

func TestFullTypingSignalerFlagsPerModeWithHeartbeatDisabled(t *testing.T) {
	for _, tc := range []struct {
		mode      TypingMode
		heartbeat bool
		immediate bool
		message   bool
		text      bool
		reasoning bool
		disabled  bool
	}{
		{mode: TypingModeInstant, immediate: true, text: true},
		{mode: TypingModeMessage, message: true, text: true},
		{mode: TypingModeThinking, reasoning: true},
		{mode: TypingModeNever, disabled: true},
		{mode: TypingModeInstant, heartbeat: true, immediate: true, text: true, disabled: true},
	} {
		controller := NewTypingController(TypingControllerConfig{})
		signaler := NewFullTypingSignaler(controller, tc.mode, tc.heartbeat)
		if signaler.ShouldStartImmediately != tc.immediate ||
			signaler.ShouldStartOnMessageStart != tc.message ||
			signaler.ShouldStartOnText != tc.text ||
			signaler.ShouldStartOnReasoning != tc.reasoning || signaler.disabled != tc.disabled {
			t.Errorf("mode %q heartbeat=%v signaler=%+v", tc.mode, tc.heartbeat, signaler)
		}
		controller.Cleanup()
	}
}

func TestFullTypingSignalerMessageModeStartsOnFirstRenderableText(t *testing.T) {
	var starts atomic.Int32
	controller := NewTypingController(TypingControllerConfig{OnStart: func() { starts.Add(1) }})
	signaler := NewFullTypingSignaler(controller, TypingModeMessage, false)
	signaler.SignalRunStart()
	signaler.SignalMessageStart()
	if starts.Load() != 0 {
		t.Fatal("message mode started before renderable text")
	}
	signaler.SignalTextDelta("NO_")
	signaler.SignalMessageStart()
	if starts.Load() != 0 || signaler.hasRenderableText.Load() {
		t.Fatal("silent token prefix was treated as renderable")
	}
	signaler.SignalTextDelta("hello")
	if starts.Load() != 1 || !signaler.hasRenderableText.Load() {
		t.Fatal("visible delta did not start message mode")
	}
	signaler.SignalMessageStart()
	if starts.Load() != 1 {
		t.Fatal("message start restarted active controller")
	}
	signaler.Stop()
	if controller.IsActive() {
		t.Fatal("signaler Stop left controller active")
	}
}

func TestFullTypingSignalerThinkingModeToolProgressRestartsAfterTTLExpiry(t *testing.T) {
	controller := NewTypingController(TypingControllerConfig{IntervalMs: 2, TtlMs: 20})
	defer controller.Cleanup()
	signaler := NewFullTypingSignaler(controller, TypingModeThinking, false)
	signaler.SignalReasoningDelta()
	if controller.IsStarted() {
		t.Fatal("thinking started before renderable text")
	}
	signaler.SignalTextDelta("analysis")
	if !controller.IsActive() {
		t.Fatal("thinking mode did not start on visible text")
	}
	controller.mu.Lock() // keepalive goroutine reads ttlDeadline under mu
	controller.ttlDeadline = time.Now().Add(-time.Second)
	controller.mu.Unlock()
	waitFor(t, 100*time.Millisecond, func() bool { return !controller.IsActive() }, "thinking TTL expiry")
	signaler.SignalToolProgress(45)
	if !controller.IsActive() {
		t.Fatal("tool progress did not restart expired controller")
	}
	signaler.SignalToolStart()
	signaler.SignalReasoningDelta()
}

func TestFullTypingSignalerNilAndDisabledMethodsAreSafe(t *testing.T) {
	nilController := NewFullTypingSignaler(nil, TypingModeInstant, false)
	nilController.SignalRunStart()
	nilController.SignalMessageStart()
	nilController.SignalTextDelta("text")
	nilController.SignalReasoningDelta()
	nilController.SignalToolStart()
	nilController.SignalToolProgress(1)
	nilController.Stop()

	var starts atomic.Int32
	controller := NewTypingController(TypingControllerConfig{OnStart: func() { starts.Add(1) }})
	defer controller.Cleanup()
	disabled := NewFullTypingSignaler(controller, TypingModeNever, false)
	disabled.SignalRunStart()
	disabled.SignalMessageStart()
	disabled.SignalTextDelta("text")
	disabled.SignalReasoningDelta()
	disabled.SignalToolStart()
	disabled.SignalToolProgress(1)
	if starts.Load() != 0 {
		t.Fatalf("disabled signaler started %d times", starts.Load())
	}
}

func TestTypingControllerConcurrentSignalsAndStop(t *testing.T) {
	var starts, stops, cleanups atomic.Int32
	controller := NewTypingController(TypingControllerConfig{
		IntervalMs: 2,
		TtlMs:      50,
		OnStart:    func() { starts.Add(1) },
		OnStop:     func() { stops.Add(1) },
		OnCleanup:  func() { cleanups.Add(1) },
	})
	signaler := NewFullTypingSignaler(controller, TypingModeInstant, false)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				switch i % 7 {
				case 0:
					signaler.SignalRunStart()
				case 1:
					signaler.SignalTextDelta("text")
				case 2:
					signaler.SignalToolStart()
				case 3:
					signaler.SignalToolProgress(j)
				case 4:
					controller.RefreshTypingTTL()
				case 5:
					_ = controller.IsActive()
				default:
					controller.Cleanup()
				}
			}
		}(i)
	}
	wg.Wait()
	controller.Cleanup()
	if controller.IsActive() || stops.Load() > 1 || cleanups.Load() != 1 {
		t.Fatalf("final state active=%v starts=%d stops=%d cleanups=%d", controller.IsActive(), starts.Load(), stops.Load(), cleanups.Load())
	}
}

func TestResolveTypingModeAndPolicyUnknownConfiguredValues(t *testing.T) {
	if got := ResolveTypingMode(TypingModeContext{Configured: TypingMode("custom")}); got != "custom" {
		t.Fatalf("configured custom mode = %q", got)
	}
	if got := ResolveTypingMode(TypingModeContext{Configured: TypingModeInstant, SuppressTyping: true}); got != TypingModeNever {
		t.Fatalf("suppression did not override configured mode: %q", got)
	}
	resolved := ResolveRunTypingPolicy(ResolveRunTypingPolicyParams{
		RequestedPolicy: types.TypingPolicy("custom"),
		SuppressTyping:  true,
	})
	if resolved.TypingPolicy != "custom" || !resolved.SuppressTyping {
		t.Fatalf("custom policy = %+v", resolved)
	}
}

func TestTrimWhitespaceASCIIContract(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: " \t\n\r ", want: ""},
		{input: " \thello\r\n", want: "hello"},
		{input: "hello world", want: "hello world"},
		{input: "\u00a0hello\u00a0", want: "\u00a0hello\u00a0"},
	} {
		if got := trimWhitespace(tc.input); got != tc.want {
			t.Errorf("trimWhitespace(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
