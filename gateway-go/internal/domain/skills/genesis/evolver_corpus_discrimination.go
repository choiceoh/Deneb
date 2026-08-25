// evolver_corpus_discrimination.go — proposal-side repairs for the L1
// starvation measured 2026-08-25.
//
// The ledger showed a month with ZERO accepted evolves (last `evolved`
// 07-20) against a steady proposal stream: 2026-06 accepted 23, 07 accepted
// 5, 08 accepted 0 with 27 rejections. Two mechanical leaks explain most of
// it, and both live on the PROPOSAL side — the acceptance gates themselves
// are working as designed and stay untouched (forbidden surface):
//
//  1. The evolve loop re-submitted BYTE-IDENTICAL candidates it had already
//     rejected (the same 유튜브/watch body ran the full replay+judge+held-out
//     pipeline three times, tying 83.3 vs 83.3 each time). The rejected-edit
//     buffer already rides the rewrite prompt as soft steering; when the
//     model ignores it, nothing refused the repeat deterministically.
//
//  2. Held-out TIES: a candidate scoring exactly the original's score can
//     never clear the strict-improvement margin. A tie repeated across
//     attempts is not evidence the candidates are bad — it is evidence the
//     validation corpus CANNOT DISCRIMINATE for that skill (no stored case
//     separates original from candidate). That is gate saturation in the
//     gate_discrimination.go sense, and the honest fix is more discriminating
//     cases, never a relaxed margin. The detector below converts repeated
//     ties into a self-correction draft asking the sweep to mine exactly
//     those cases. Complements the existing tie-disclosure lane
//     (evolver_tie_disclosure.go), which surfaces ALREADY-STORED unmet
//     requirements to the producer — that lane ran all August and the ties
//     persisted, i.e. the stored cases alone cannot separate; the missing
//     demand is NEW cases mined from real failures.
package genesis

import (
	"fmt"
	"strings"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
)

const (
	// repeatRefusalScanLimit bounds the rejected-buffer scan per candidate.
	repeatRefusalScanLimit = 8
	// repeatRefusalMinRunes guards against degenerate matches: a stub body
	// this short carries no mutation identity worth refusing on.
	repeatRefusalMinRunes = 200

	// heldOutTieWindow / heldOutTieThreshold: this many tie rejections for
	// one skill inside the window ⇒ the corpus, not the candidates, is the
	// bottleneck.
	heldOutTieWindow    = 14 * 24 * time.Hour
	heldOutTieThreshold = 3
	// heldOutTieDraftSource is the dedup signature prefix for the drafts.
	heldOutTieDraftSource = "held-out-tie-corpus"
)

// refuseRepeatedRejectedCandidate reports whether candidateBody is a repeat of
// a recently rejected candidate for the skill, with the refusal reason. The
// buffer stores bodies truncated at rejectedEditBodyLimit, so comparison is on
// the stored prefix — byte-identical up to what the buffer kept counts as the
// same mutation. Fail-open: no tracker, a read error, or a short body never
// refuses.
func (e *Evolver) refuseRepeatedRejectedCandidate(skillName, candidateBody string) (string, bool) {
	if e.tracker == nil {
		return "", false
	}
	candidate := strings.TrimSpace(candidateBody)
	if len([]rune(candidate)) < repeatRefusalMinRunes {
		return "", false
	}
	recent, err := e.tracker.RecentRejectedSkillEdits(skillName, repeatRefusalScanLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: repeat-refusal scan failed", "skill", skillName, "error", err)
		}
		return "", false
	}
	candidateStored := strings.TrimSpace(genesiscommon.TruncateRunes(candidate, rejectedEditBodyLimit))
	for _, rec := range recent {
		// Infrastructure rows are evidence of an outage, not a quality
		// verdict — the "avoid repeating this mutation" contract explicitly
		// excludes them (tracker_rejected_edits.go).
		if rec.Infrastructure {
			continue
		}
		stored := strings.TrimSpace(rec.CandidateBody)
		if stored == "" || len([]rune(stored)) < repeatRefusalMinRunes {
			continue
		}
		if stored == candidateStored {
			return fmt.Sprintf("repeat refusal: candidate is byte-identical to a recently rejected attempt (reason then: %s)",
				genesiscommon.TruncateRunes(rec.Reason, 200)), true
		}
	}
	return "", false
}

// queueHeldOutTieCorpusDraft promotes repeated held-out TIES into one
// structural self-correction draft: the validation corpus for this skill
// cannot discriminate, so the sweep should mine cases that do. Sibling of
// queueRepeatedPatchFirstReviewDraft — same scan, threshold, and reopen-guard
// shape.
func (e *Evolver) queueHeldOutTieCorpusDraft(skillName, reason, source string) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" || e.tracker == nil || !isHeldOutTieRejection(reason) {
		return
	}
	recent, err := e.tracker.RecentRejectedSkillEdits(skillName, repeatRefusalScanLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: held-out tie scan failed", "skill", skillName, "error", err)
		}
		return
	}
	cutoff := time.Now().Add(-heldOutTieWindow).UnixMilli()
	ties := make([]RejectedSkillEditRecord, 0, len(recent))
	var freshLastAt int64
	for _, rec := range recent {
		if rec.CreatedAt >= cutoff && isHeldOutTieRejection(rec.Reason) {
			ties = append(ties, rec)
			if rec.CreatedAt > freshLastAt {
				freshLastAt = rec.CreatedAt
			}
		}
	}
	// The triggering rejection is appended by the caller before this runs, but
	// byte-identical (skill, reason) rows collapse in the tracker's dedupe —
	// count the live reason in when the collapse hid it.
	if !containsTieReason(ties, reason) {
		ties = append(ties, RejectedSkillEditRecord{Reason: reason, CreatedAt: time.Now().UnixMilli()})
		freshLastAt = time.Now().UnixMilli()
	}
	if len(ties) < heldOutTieThreshold {
		return
	}
	existing, err := e.tracker.RecentSelfCorrectionCandidates(skillName, "", 50)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: held-out tie dedup scan failed", "skill", skillName, "error", err)
		}
		return
	}
	if selfCorrectionReopenBlocked(existing, heldOutTieDraftSource, freshLastAt, time.Now()) {
		return
	}
	evidence := make([]string, 0, len(ties)+1)
	evidence = append(evidence, fmt.Sprintf("%d held-out TIE rejections within %dd (candidate score == original — the corpus separated nothing):",
		len(ties), int(heldOutTieWindow.Hours()/24)))
	for _, rec := range ties {
		evidence = append(evidence, "- "+genesiscommon.TruncateRunes(rec.Reason, 240))
	}
	if _, err := e.tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:     "test",
		SkillName: skillName,
		Title:     "held-out 코퍼스가 변별하지 못함 — 가르는 케이스를 채굴할 것",
		Candidate: fmt.Sprintf("%s 의 최근 진화가 held-out 검증에서 전부 원본과 동점입니다. 그래서 어떤 개선도 반영될 수 없습니다(후보 품질이 아니라 게이트 포화). 이 스킬의 최근 실제 실패에서 새 검증 케이스를 채굴해야 합니다 — 지금 본문이 실제로 실패하는 케이스여야 진짜 수정이 원본보다 높은 점수를 받을 수 있습니다.",
			skillName),
		Evidence:       strings.Join(evidence, "\n"),
		Reason:         reason,
		ProposedChange: "지금 스킬 본문이 실패하는 held-out 검증 케이스를 실제 실패 트레이스에서 뽑아 추가한다 — 엄격 개선 마진이 잴 것이 생기도록. skillHeldOutMinScoreDelta를 완화하거나 동점 후보를 수용하면 안 된다.",
		Risk:           "케이스 채굴만 하며 수용 게이트는 그대로입니다. 채굴한 케이스는 실제로 관측된 실패여야 하고, 대기 중인 후보를 통과시키려고 맞춘 합성 단언이면 안 됩니다.",
		Source:         heldOutTieDraftSource + ":" + strings.TrimSpace(source),
	}); err != nil && e.logger != nil {
		e.logger.Warn("evolver: held-out tie draft failed", "skill", skillName, "error", err)
	}
}

func containsTieReason(ties []RejectedSkillEditRecord, reason string) bool {
	for _, rec := range ties {
		if rec.Reason == reason {
			return true
		}
	}
	return false
}
