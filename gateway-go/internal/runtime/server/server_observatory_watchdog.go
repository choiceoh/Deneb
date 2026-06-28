package server

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/observatory"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

const (
	// observatoryWatchdogInterval is how often we check our own improvement
	// loops. The fleetAlertGate suppresses unchanged repeats, so a tight tick
	// sharpens detection latency without spamming.
	observatoryWatchdogInterval = 30 * time.Minute
	// observatoryWatchdogStartDelay lets boot transients settle before the first
	// check — a just-restarted gateway has not re-touched every loop's state yet.
	observatoryWatchdogStartDelay = 5 * time.Minute
	// observatoryFailAlertThreshold is the 24h silent-failure count that warrants
	// a push; below it is normal LLM-quirk noise.
	observatoryFailAlertThreshold = 5
)

type watchdogAlert struct {
	Title string
	Level string
	Body  string
}

// watchdogAlerts derives the push-worthy conditions from a snapshot: an
// improvement loop gone stale, or a silent-failure pattern that has spiked.
// Titles are stable (no changing age/count) so the alert gate keys on the
// condition, not the reading. Pure, so it is unit-testable.
func watchdogAlerts(rep observatory.Report, failThreshold int) []watchdogAlert {
	var out []watchdogAlert
	for _, l := range rep.Liveness {
		if l.Fresh || l.Missing {
			continue
		}
		out = append(out, watchdogAlert{
			Title: "개선 루프 정지: " + l.Name,
			Level: "warn",
			Body:  fmt.Sprintf("%s 루프가 %s째 갱신 없음 — 자기개선 텔레메트리 점검 필요 (observe action=health).", l.Name, humanHours(l.AgeHours)),
		})
	}
	for _, f := range rep.Failures {
		if f.Count < failThreshold {
			continue
		}
		out = append(out, watchdogAlert{
			Title: "침묵 실패 급증: " + f.Pattern,
			Level: "warn",
			Body:  fmt.Sprintf("최근 24h에 %s ×%d — 조용히 떨어지는 호출/결정.", f.Pattern, f.Count),
		})
	}
	return out
}

func humanHours(h float64) string {
	if h >= 48 {
		return fmt.Sprintf("%d일", int(h/24))
	}
	return fmt.Sprintf("%d시간", int(h))
}

// observatoryWatchdogTick runs one self-liveness check and pushes any new
// condition through the same gated path as the fleet hook.
func (s *Server) observatoryWatchdogTick(now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in observatory watchdog tick", "panic", r)
		}
	}()
	rep := observatory.Snapshot(config.ResolveStateDir(), now)
	for _, a := range watchdogAlerts(rep, observatoryFailAlertThreshold) {
		if s.fleetAlerts != nil && !s.fleetAlerts.shouldRelay(a.Title, a.Level, now) {
			continue
		}
		if s.pushHub != nil {
			s.pushHub.publish(clientPushEvent{Title: "⚠️ 자기점검 · " + a.Title, Body: a.Body, Kind: pushKindFleet})
		}
		s.logger.Warn("observatory watchdog alert", "title", a.Title)
	}
}

// runObservatoryWatchdog periodically checks whether Deneb's own improvement
// loops have gone silent — the dreamer/skill-curator/config-audit deaths that
// rotted unnoticed for weeks. It mirrors the fleet hook (which exists for the
// same reason: a silent SparkFleet death), gated by [fleetAlertGate] against the
// over-notification the project forbids. Stops when ctx is canceled.
func (s *Server) runObservatoryWatchdog(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(observatoryWatchdogStartDelay):
	}
	ticker := time.NewTicker(observatoryWatchdogInterval)
	defer ticker.Stop()
	for {
		s.observatoryWatchdogTick(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
