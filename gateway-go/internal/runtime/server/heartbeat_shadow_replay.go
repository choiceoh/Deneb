// heartbeat_shadow_replay.go — P1 of the instruction-surface evolve program
// (docs/research/instruction-surface-evolve.md): a DRY-RUN gate that replays
// harvested heartbeat fixtures (heartbeat_fixtures.go) under the as-fired
// contract vs a candidate HEARTBEAT.md and scores both with deterministic
// verifiers. Nothing is applied — the report is evidence to attach to a
// propose-only candidate. The same executor runs both sides so executor bias
// cancels (the skill replay gate's argument, init_genesis.go).
package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	// heartbeatShadowDefaultFixtures bounds the replay cost per dry run
	// (2 completions per fixture). Callers can lower it, never raise past cap.
	heartbeatShadowDefaultFixtures = 10
	heartbeatShadowMaxFixtures     = 20
	// heartbeatShadowMinFixtures keeps verdicts honest: with fewer usable
	// fixtures than this, the report says "insufficient corpus" instead of
	// passing judgment on noise.
	heartbeatShadowMinFixtures = 4
	// heartbeatShadowOutputBudget mirrors the output-safety budget the quality
	// suite enforces on real turns.
	heartbeatShadowOutputBudget = 4096
)

// shadowCompleteFunc is the injected text-only executor (production: the
// lightweight role; tests: a fake). No tools are offered — the shadow turn is
// a decision-shape probe, not a real run.
type shadowCompleteFunc func(ctx context.Context, system, user string) (string, error)

// heartbeatShadowSystemPrompt is the compact stand-in for the full production
// system prompt. Identical on both sides of every comparison, so its distance
// from production cancels out of the verdict.
const heartbeatShadowSystemPrompt = `당신은 Deneb의 하트비트 점검 턴을 시뮬레이션합니다. 도구는 사용할 수 없습니다.
아래 자가 점검 메시지에 대해, 실제 턴이 사용자에게 남길 최종 텍스트만 출력하세요.
알릴 것이 없으면 정확히 NO_REPLY 한 단어만 출력하세요.`

// heartbeatShadowFixtureResult is one fixture's paired outcome.
type heartbeatShadowFixtureResult struct {
	FiredAt       int64  `json:"firedAt"`
	Split         string `json:"split"` // "held-in" | "held-out"
	Quiet         bool   `json:"quiet"` // recorded real outcome was NO_REPLY
	OriginalPass  bool   `json:"originalPass"`
	CandidatePass bool   `json:"candidatePass"`
	Note          string `json:"note,omitempty"`
}

// heartbeatShadowReport is the dry-run verdict: pass counts per split for the
// as-fired contracts vs the candidate, the no-trade-off decision, and
// per-fixture rows for auditability.
type heartbeatShadowReport struct {
	OK                bool                           `json:"ok"`
	Verdict           string                         `json:"verdict"` // "accept" | "reject" | "insufficient-corpus"
	Reason            string                         `json:"reason"`
	Fixtures          int                            `json:"fixtures"`
	HeldInOriginal    int                            `json:"heldInOriginal"`
	HeldInCandidate   int                            `json:"heldInCandidate"`
	HeldInTotal       int                            `json:"heldInTotal"`
	HeldOutOriginal   int                            `json:"heldOutOriginal"`
	HeldOutCandidate  int                            `json:"heldOutCandidate"`
	HeldOutTotal      int                            `json:"heldOutTotal"`
	Results           []heartbeatShadowFixtureResult `json:"results,omitempty"`
	DryRun            bool                           `json:"dryRun"` // always true in P1
	ContractDriftNote string                         `json:"contractDriftNote,omitempty"`
}

// runHeartbeatShadowReplay loads the newest usable fixtures, splits them
// held-in/held-out by time (older half in), replays original vs candidate,
// and applies the no-trade-off acceptance rule over verifier pass counts.
func runHeartbeatShadowReplay(ctx context.Context, fixturePath, candidate string, limit int, complete shadowCompleteFunc) (heartbeatShadowReport, error) {
	report := heartbeatShadowReport{DryRun: true}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return report, fmt.Errorf("heartbeat shadow replay: candidate content is required")
	}
	if complete == nil {
		return report, fmt.Errorf("heartbeat shadow replay: executor is not configured")
	}
	if limit <= 0 || limit > heartbeatShadowMaxFixtures {
		if limit > heartbeatShadowMaxFixtures {
			limit = heartbeatShadowMaxFixtures
		} else {
			limit = heartbeatShadowDefaultFixtures
		}
	}

	entries, err := jsonlstore.Load[heartbeatFixture](fixturePath)
	if err != nil {
		return report, fmt.Errorf("heartbeat shadow replay: load fixtures: %w", err)
	}
	usable := make([]heartbeatFixture, 0, len(entries))
	hashes := map[string]bool{}
	for _, f := range entries {
		// Error-outcome fixtures carry no decision ground truth; skip them.
		if strings.TrimSpace(f.OutcomeText) == "" {
			continue
		}
		usable = append(usable, f)
		if f.HeartbeatHash != "" {
			hashes[f.HeartbeatHash] = true
		}
	}
	if len(usable) > limit {
		usable = usable[len(usable)-limit:] // newest window (append order)
	}
	report.Fixtures = len(usable)
	if len(usable) < heartbeatShadowMinFixtures {
		report.Verdict = "insufficient-corpus"
		report.Reason = fmt.Sprintf("usable fixtures %d < %d — let the harvest lane accumulate more firings before judging", len(usable), heartbeatShadowMinFixtures)
		return report, nil
	}
	if len(hashes) > 1 {
		report.ContractDriftNote = fmt.Sprintf("fixtures span %d contract versions; each replays against its own as-fired contract", len(hashes))
	}

	sort.SliceStable(usable, func(i, j int) bool { return usable[i].FiredAt < usable[j].FiredAt })
	heldInCount := len(usable) / 2

	for i, f := range usable {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		split := "held-out"
		if i < heldInCount {
			split = "held-in"
		}
		quiet := isHeartbeatQuietOutcome(f.OutcomeText)

		origOut, origErr := complete(ctx, heartbeatShadowSystemPrompt, heartbeatShadowTrigger(f, f.HeartbeatMD))
		candOut, candErr := complete(ctx, heartbeatShadowSystemPrompt, heartbeatShadowTrigger(f, candidate))
		result := heartbeatShadowFixtureResult{FiredAt: f.FiredAt, Split: split, Quiet: quiet}
		result.OriginalPass, _ = verifyHeartbeatShadowOutput(quiet, origOut, origErr)
		var note string
		result.CandidatePass, note = verifyHeartbeatShadowOutput(quiet, candOut, candErr)
		if !result.CandidatePass {
			result.Note = note
		}

		if split == "held-in" {
			report.HeldInTotal++
			if result.OriginalPass {
				report.HeldInOriginal++
			}
			if result.CandidatePass {
				report.HeldInCandidate++
			}
		} else {
			report.HeldOutTotal++
			if result.OriginalPass {
				report.HeldOutOriginal++
			}
			if result.CandidatePass {
				report.HeldOutCandidate++
			}
		}
		report.Results = append(report.Results, result)
	}

	deltaIn := report.HeldInCandidate - report.HeldInOriginal
	deltaOut := report.HeldOutCandidate - report.HeldOutOriginal
	switch {
	case deltaIn < 0 || deltaOut < 0:
		report.Verdict = "reject"
		report.Reason = fmt.Sprintf("no-trade-off rule: candidate regresses a split (Δin=%d, Δout=%d)", deltaIn, deltaOut)
	case deltaIn == 0 && deltaOut == 0:
		report.Verdict = "reject"
		report.Reason = "no-trade-off rule: no measurable improvement on either split"
	default:
		report.OK = true
		report.Verdict = "accept"
		report.Reason = fmt.Sprintf("candidate improves without regression (Δin=%d, Δout=%d) — dry-run evidence only; application stays propose-only", deltaIn, deltaOut)
	}
	return report, nil
}

// heartbeatShadowTrigger re-assembles the firing's trigger with the given
// contract — the fixture's variable inputs stay identical across both sides.
func heartbeatShadowTrigger(f heartbeatFixture, contract string) string {
	return fmt.Sprintf(heartbeatTriggerTemplate,
		composeHeartbeatBody(f.SignalSummary, contract, f.SelfCodingNudge, f.SweepNudge, f.ResearchNudge))
}

func isHeartbeatQuietOutcome(outcome string) bool {
	return strings.TrimSpace(outcome) == "NO_REPLY"
}

// verifyHeartbeatShadowOutput applies the deterministic verifiers:
// quiet fixtures must stay quiet (exactly NO_REPLY), actionable fixtures must
// not go silent, and every output must respect the safety budget (length,
// no internal-token leakage). Judge scoring is deliberately absent — see the
// design note's weak-evaluator caution.
func verifyHeartbeatShadowOutput(quiet bool, output string, err error) (bool, string) {
	if err != nil {
		return false, "executor error: " + err.Error()
	}
	trimmed := strings.TrimSpace(output)
	if quiet {
		if trimmed != "NO_REPLY" {
			return false, "quiet fixture: expected exactly NO_REPLY"
		}
		return true, ""
	}
	if trimmed == "" || trimmed == "NO_REPLY" {
		return false, "actionable fixture: candidate went silent"
	}
	if len([]rune(trimmed)) > heartbeatShadowOutputBudget {
		return false, "output budget exceeded"
	}
	for _, leak := range []string{"<function=", "<thinking>", "<system-reminder>"} {
		if strings.Contains(trimmed, leak) {
			return false, "internal token leaked: " + leak
		}
	}
	return true, ""
}
