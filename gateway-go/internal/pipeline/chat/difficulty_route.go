// difficulty_route.go — difficulty-based main/main2 model routing.
//
// With two main-tier subscriptions (main = flagship for analysis-grade
// intelligence, main2 = the stable workhorse), the axis that decides which
// one a turn deserves is DIFFICULTY, not where the turn came from (operator
// design call, 2026-07-17: "대화형=고지능, 배경=저지능"이 아니라 난이도가 축).
// Obviously-simple interactive turns — the bulk of chat volume — ride main2,
// reserving the flagship's subscription quota for turns that need it.
//
// The simplicity verdict reuses the effort router's calibrated heuristic
// (router.Decide): short conversational message, no attachments, no
// automation, and no heavy recent context (a long assistant reply right
// before "응 계속해" means mid-deep-work — stays on main). That history
// signal is why this routes AFTER message assembly, not inside resolveModel.
//
// The swap happens before any consumer of the resolved model (APC diag,
// tuning, cache policy, agent config), so downstream behaves exactly as a
// native main2 turn — including the mutual fallback chain: a failing routed
// turn escalates main2 → main, so a wrong "simple" verdict degrades to the
// flagship, never below the main tier.
//
// Gating: DENEB_ADAPTIVE_EFFORT on (same opt-in as the effort router) AND
// agents.main2Model configured. Unset main2 = feature fully off.
package chat

import (
	"log/slog"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
)

// mainToMain2TargetRatio is the operator's traffic split target: main1 gets
// about this many main-tier turns for each main2 turn ("한 2:1 정도로 메인1에
// 더", 2026-07-17). The governor below caps main2's share at 1/(1+ratio) of
// counted main-tier turns — a simple turn stays on main1 while main2 is at or
// over its share, so the split self-balances regardless of the day's
// simple/analytical mix (main2 can run UNDER its share when simple turns are
// scarce; it never runs over).
const mainToMain2TargetRatio = 2

// mainTierTurns / main2Turns count main-tier turn routing since boot (atomic —
// turns run concurrently). In-memory only: a restart re-runs the short
// cold-start holdback, which is noise at this cadence.
var (
	mainTierTurns atomic.Int64 // turns kept on main1 (flagship)
	main2Turns    atomic.Int64 // turns routed to main2
)

// underMain2Share reports whether routing ONE more turn to main2 keeps it at
// or under its target share. Evaluated on the post-route totals so the
// steady-state cadence is exact (route every (ratio+1)-th eligible turn when
// every turn is simple).
func underMain2Share() bool {
	m1 := mainTierTurns.Load()
	m2 := main2Turns.Load()
	return (m2+1)*(mainToMain2TargetRatio+1) <= m1+m2+1
}

// sessionSpawnedBy is the nil-safe sub-agent marker accessor for the route's
// call site (a spawned session must never be rerouted off its model).
func sessionSpawnedBy(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return sess.SpawnedBy
}

// difficultyRoute carries the main2 resolution a routed turn switches to.
type difficultyRoute struct {
	model      string
	providerID string
	client     *llm.Client
	reason     string
}

// difficultyModelRoute returns the main2 route for an obviously-simple
// interactive main turn, or nil to keep the resolved main model. Pure given
// the registry — no IO beyond the registry's cached client build.
func difficultyModelRoute(
	reg *modelrole.Registry,
	params RunParams,
	spawnedBy string,
	messages []llm.Message,
	initialRole modelrole.Role,
	resolvedProvider, resolvedModel string,
	logger *slog.Logger,
) *difficultyRoute {
	if effortMode() == effortModeOff || reg == nil {
		return nil
	}
	// Only genuine main-role turns are candidates: sub-agent sessions and
	// non-main roles (vision, coding) keep their resolution and don't count
	// toward the main-tier split.
	if initialRole != modelrole.RoleMain || spawnedBy != "" {
		return nil
	}
	// The turn must actually be about to run the flagship main — a session that
	// resolved elsewhere (defaultModel drift, raw override upstream) is not ours
	// to reroute or count.
	full := resolvedModel
	if resolvedProvider != "" {
		full = resolvedProvider + "/" + resolvedModel
	}
	if full != reg.FullModelID(modelrole.RoleMain) {
		return nil
	}
	m2 := reg.FullModelID(modelrole.RoleMain2)
	if m2 == "" || m2 == full {
		return nil
	}
	// From here every outcome consumes a main-tier subscription — count it so
	// the ratio governor sees the whole main-tier mix (automation and
	// analytical turns push the split toward main1; simple turns then flow to
	// main2 only up to its share).
	if params.Model != "" || isAutomationRun(params) || len(params.Attachments) > 0 {
		mainTierTurns.Add(1)
		return nil
	}
	// Difficulty verdict via the effort router's heuristic with its default
	// calibrated thresholds. Deliberately NOT the main model's routing profile:
	// that profile's Enabled/toggle fields describe thinking-toggle capability
	// (irrelevant here), and its operator tunings target thinking routing.
	dec := leafbind.Decide(leafbind.DefaultProfile(), leafbind.Request{
		Message: params.Message,
		History: messages,
	})
	if !dec.ThinkingOff {
		mainTierTurns.Add(1)
		return nil // not obviously simple → flagship
	}
	if !underMain2Share() {
		mainTierTurns.Add(1)
		if logger != nil {
			logger.Info("difficulty route: simple turn held on main (ratio)",
				"reason", dec.Reason, "main1Turns", mainTierTurns.Load(), "main2Turns", main2Turns.Load())
		}
		return nil
	}
	client := reg.Client(modelrole.RoleMain2)
	if client == nil {
		mainTierTurns.Add(1)
		return nil
	}
	main2Turns.Add(1)
	providerID, modelName := modelrole.ParseModelID(m2)
	if logger != nil {
		logger.Info("difficulty route: simple turn → main2",
			"reason", dec.Reason, "model", modelName, "keptFrom", resolvedModel)
	}
	return &difficultyRoute{
		model:      modelName,
		providerID: providerID,
		client:     client,
		reason:     dec.Reason,
	}
}
