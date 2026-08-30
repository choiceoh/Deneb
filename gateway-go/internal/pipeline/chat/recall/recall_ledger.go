// recall_ledger.go — per-turn evidence bookkeeping for the recall preflight:
// the evidence budget, the dedup/broadening passes, and the miss/utility ledger
// writes that feed recall-bench. Gathering lives in recall_evidence.go and
// ranking in recall_preflight.go.

package recall

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func recallEvidenceBudget(cue bool) int {
	// DENEB_RECALL_MAX_EVIDENCE overrides BOTH budgets — the evidence-budget
	// sweep knob (rows × note cap is the axis the reader-accuracy curve is
	// measured over; the operator decides the production point from that curve).
	if raw := strings.TrimSpace(os.Getenv("DENEB_RECALL_MAX_EVIDENCE")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 4 && v <= 24 {
			return v
		}
	}
	if cue {
		return recallMaxEvidence
	}
	return recallAutoMaxEvidence
}

// recallPrimaryQuery returns the combined multi-term query (expressing the full
// user intent) that searchQueries emits, or "" when the message had only
// one signal term. The combined query is the sole space-joined entry; tokenized
// single terms never contain spaces.
func recallPrimaryQuery(queries []string) string {
	for _, q := range queries {
		if strings.Contains(q, " ") {
			return q
		}
	}
	return ""
}

// applyBroadeningPenalty demotes evidence found only by an individual
// broadening term below combined-query hits (see the call site for rationale).
// Rows tagged with an anchor sentinel (project/counterparty) keep their score:
// anchors are pinned structurally, not found by a term.
func applyBroadeningPenalty(evidence []recallEvidence, queries []string) {
	primary := recallPrimaryQuery(queries)
	if primary == "" {
		return
	}
	for i := range evidence {
		q := evidence[i].Query
		if q != "" && q != primary && q != recallProjectAnchorQuery && q != recallCounterpartyAnchorQuery {
			evidence[i].Score *= recallBroadeningPenalty
		}
	}
}

// dedupRecallEvidence collapses rows describing the same content surfaced via
// different sources, keeping the best-scored row. Keyed on a normalized note
// prefix: refs differ across sources for the same fact, the words don't.
func dedupRecallEvidence(evidence []recallEvidence) []recallEvidence {
	if len(evidence) <= 1 {
		return evidence
	}
	bestIdx := make(map[string]int, len(evidence))
	out := evidence[:0]
	for _, ev := range evidence {
		key := recallContentKey(ev.Note)
		if key == "" {
			out = append(out, ev)
			continue
		}
		if i, ok := bestIdx[key]; ok {
			if ev.Score > out[i].Score {
				out[i] = ev
			}
			continue
		}
		bestIdx[key] = len(out)
		out = append(out, ev)
	}
	return out
}

// recallContentKey normalizes a note for duplicate detection: lowercase,
// letters/digits only, first 80 runes.
func recallContentKey(note string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.ToLower(note) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 80 {
			break
		}
	}
	return b.String()
}

// isLedgerPage reports whether an evidence row points at a real wiki page the
// utility ledger should score.
//
// Kind is the SOURCE that produced the row, not the artifact it points at: an
// 인물 page pulled in through the org chart arrives as Kind "org" carrying the
// page's relPath in Source (recall_org.go: resolveOrgPersonPaths). Recording
// only Kind=="wiki" therefore made person pages structurally invisible to the
// ledger — measured 2026-07-27: the org source fired on 12% of preflights over
// 7 days and contributed ZERO ledger lines, so the utility report read 인물 as
// 2% used (255 pages) when that number was really coverage, not usage. Judge by
// what Source points at: a page path, not an "조직도: 이름" placeholder for a
// member with no page yet.
func isLedgerPage(ev recallEvidence) bool {
	if ev.Source == "" {
		return false
	}
	switch ev.Kind {
	case "wiki":
		return true
	case "org":
		// Org rows carry either a page relPath or a "조직도: <이름>" label for a
		// member the wiki has no page for; only the former is a page.
		return strings.HasSuffix(ev.Source, ".md")
	}
	return false
}

// recordRecallMiss tees an unanswered CUE turn into the demand ledger (wiki/
// recall_misses.go) — the supply side (recordRecallUtility) records which pages
// got used, this records which questions had no page at all. Best-effort: a nil
// store or a write error is swallowed after a single Warn.
func recordRecallMiss(store *wiki.Store, sessionKey, message string, logger *slog.Logger) {
	if store == nil || strings.TrimSpace(message) == "" {
		return
	}
	if err := store.RecordRecallMiss(wiki.RecallMiss{Query: message, Session: sessionKey}); err != nil && logger != nil {
		logger.Warn("recall preflight: demand ledger write failed", "error", err)
	}
}

// recordRecallMissIfHuman records demand only for turns a PERSON drove.
//
// The ledger answers "what did the user ask that memory could not", and the
// research lane spends its budget on the answer. EphemeralUser marks a
// machine-originated turn — phone-event ingest, heartbeat, boot, mail QA —
// whose "question" is an injected template. Counting those lets the system read
// its own scaffolding back as user demand: on 2026-08-30 the live ledger held
// exactly one line, a phone-event prompt, and that week's memory digest duly
// reported 실시간·스마트폰·이벤트·알림·출처 — the words of the template header —
// as 자주 물은 주제.
//
// Recall itself still runs for these turns and still injects what it finds;
// only the demand count is withheld. The same text typed by a person is real
// demand and is recorded.
func recordRecallMissIfHuman(store *wiki.Store, ephemeralUser bool, sessionKey, message string, logger *slog.Logger) {
	if ephemeralUser {
		return
	}
	recordRecallMiss(store, sessionKey, message, logger)
}

// recordRecallUtility tees the injected wiki-page evidence into the store's
// recall-utility ledger (효용 접지) as inject events, each carrying the
// retrieval context (query label, injection rank, preflight score — so
// real-traffic (query → page) pairs can be mined as gold-set candidates) and
// the session, so downstream usage events (read/cite) can be attributed
// against the exposure. Which rows count as pages is isLedgerPage's call;
// diary/transcript/file rows are not dreamer-managed pages, so they are not
// scored. Rank is the row's 1-based position in the FULL ranked evidence list
// (all kinds) — i.e. its position in the recall block the model actually saw.
// Returns the recorded paths so the caller can arm the end-of-turn citation
// pass. Best-effort: a nil store or a write error is swallowed after a single
// Warn — losing this derived telemetry is not user-observable and self-heals
// next turn.
func recordRecallUtility(store *wiki.Store, evidence []recallEvidence, sessionKey string, cue bool, logger *slog.Logger) []string {
	if store == nil || len(evidence) == 0 {
		return nil
	}
	// Turn-level gate-shadow signals (arXiv 2607.14390's episodic confidence
	// gate): the paper separates useful from harmful injections by the ranked
	// block's top-1 score and top1−top2 gap. We record the signals on every
	// inject line — production behavior is UNCHANGED — so the silence gate can
	// be calibrated offline against this ledger's read/cite outcomes before
	// any threshold ever fires (recall bottleneck doctrine: no ungrounded
	// suppression).
	top1 := evidence[0].Score
	gap := 0.0
	if len(evidence) > 1 {
		gap = top1 - evidence[1].Score
	}
	events := make([]wiki.RecallEvent, 0, len(evidence))
	paths := make([]string, 0, len(evidence))
	for i, ev := range evidence {
		if isLedgerPage(ev) {
			events = append(events, wiki.RecallEvent{
				Path:    ev.Source,
				Event:   wiki.RecallEventInject,
				Query:   ev.Query,
				Rank:    i + 1,
				Score:   ev.Score,
				Session: sessionKey,
				Top1:    top1,
				Gap:     gap,
				Cue:     cue,
			})
			paths = append(paths, ev.Source)
		}
	}
	if len(events) == 0 {
		return nil
	}
	if err := store.RecordRecallEvents(events); err != nil && logger != nil {
		logger.Warn("recall preflight: recall-hit ledger write failed", "error", err)
	}
	return paths
}
