package phoneevents

import (
	"context"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// tinyGateSystem is the push-worthiness rubric the tiny model answers in one
// word.
//
// Do not grow it into YES/NO lists without replaying both chain models. A
// longer rubric naming the categories every model missed (carrier call
// notices, failed payments, gift deliveries waiting on an address) was
// replayed on 537 real notifications (2026-09-12..18): the OpenRouter
// fallback — which answered most gate calls that week — went from passing
// 53 of 68 clear signals to 36, dropping work reports, contract threads and
// calendar reminders.
const tinyGateSystem = "당신은 스마트폰 알림 분류기다. 사용자에게 즉시 알릴 가치가 있는 업무·일정·금전·중요 연락이면 YES, " +
	"광고·프로모션·스팸·인증번호(OTP)·결제 영수증·배송/마케팅 알림·일상적 시스템/앱 알림이면 NO. YES 또는 NO 한 단어만 답하라."

// defaultTinyGateThreshold is the P(YES) a notification needs to reach the full
// judgment turn. The text gate it replaces was an implicit 0.5 (greedy decoding
// picks YES exactly when P(YES) > P(NO)). Replaying the same 537 notifications
// through the local tiny model: 0.5 passed 61 of 68 clear signals, 0.2 passed
// 65 — the four regained were a missed-call text, a failed payment with a
// same-day deadline and two gifts waiting on a delivery address — for about a
// dozen more notices a week (bank and insurance notices, a taxi status) that
// the judgment turn answers with silence. A pass costs one flat-rate turn; a
// drop is silent.
const defaultTinyGateThreshold = 0.2

// tinyGateThresholdEnv tunes the threshold without a rebuild.
const tinyGateThresholdEnv = "DENEB_PHONE_GATE_THRESHOLD"

// tinyYesNo is the gate's model call; tests swap it.
var tinyYesNo = pilot.CallTinyYesNo

// gateVerdict is the tiny gate's decision on one notification.
type gateVerdict struct {
	pass bool
	// mode is how pass was decided: "prob" (P(YES) against the threshold),
	// "token" (the one-word answer — backends without logprobs), or "error"
	// (no model answered; the gate failed open).
	mode  string
	pYes  float64 // set when mode == "prob"
	model string
	err   error
}

// tinyGateThreshold returns the configured threshold, ignoring values outside
// (0, 1) — a typo must not open or close the gate completely.
func tinyGateThreshold() float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(tinyGateThresholdEnv)), 64); err == nil && v > 0 && v < 1 {
		return v
	}
	return defaultTinyGateThreshold
}

// worthFullJudgment is the tiered-triage first pass: a cheap tiny-model
// verdict on whether a notification deserves the full tool-calling judgment
// turn. It catches the obvious noise (ads/promo/OTP/receipts/routine) the full
// judgment would also NO_REPLY, without spending a main-model turn. Fail-open —
// when no model answers, the notification passes (never silently drop signal).
func worthFullJudgment(ctx context.Context, source, text string, threshold float64) gateVerdict {
	res, err := tinyYesNo(ctx, tinyGateSystem, "앱: "+source+"\n알림 내용:\n"+text)
	if err != nil {
		return gateVerdict{pass: true, mode: "error", err: err}
	}
	if res.HasProb {
		return gateVerdict{pass: res.PYes >= threshold, mode: "prob", pYes: res.PYes, model: res.Model}
	}
	// Only an explicit NO drops, as the text gate always read it.
	return gateVerdict{pass: !strings.HasPrefix(strings.ToUpper(res.Token), "NO"), mode: "token", model: res.Model}
}

// logTinyGate records every verdict, drops included — the gate used to log
// drops at Debug only, so twelve days of journal held not one of them and its
// recall could be measured only by replaying the ledger. The notification text
// stays out of the log; the ledger already holds it.
func logTinyGate(logger *slog.Logger, source, eventType string, threshold float64, v gateVerdict) {
	attrs := []any{"source", source, "type", eventType, "pass", v.pass, "mode", v.mode, "model", v.model}
	if v.mode == "prob" {
		attrs = append(attrs, "p", math.Round(v.pYes*1000)/1000, "threshold", threshold)
	}
	if v.mode == "error" {
		logger.Warn("phone-event tiny-gate failed open", append(attrs, "error", v.err)...)
		return
	}
	logger.Info("phone-event tiny-gate", attrs...)
}
