// recall_route.go — deterministic routing hints for query shapes page search
// cannot answer.
//
// The 2026-07-25 pool-ceiling measurement (#4238) split every recall-bench miss
// into generation vs ranking misses and found the residue is neither: aggregate
// ("하반기 파이프라인 총 용량"), temporal ("마지막 메일 언제"), ledger-total
// ("SunKean 견적 총액"), and graph ("ZTT 엮인 프로젝트") questions are
// structurally unanswerable by ranked page snippets — the answer needs a
// different tool (deal_ledger / mail_archive / graphify / enumeration), i.e.
// ROUTING, not better ranking. The model already owns those tools; what it
// lacks at answer time is the nudge that this turn is one of those shapes —
// wiki evidence usually EXISTS for these queries (adjacent pages), which reads
// as "recall answered" and suppresses the tool call.
//
// Conservative by construction: pure string matching, a hint fires only when a
// recall block is already being emitted, and the hint rides OUTSIDE the
// untrusted fence (it is server guidance, not recalled data). No hint, no byte
// change — the common path is untouched.
package recall

import (
	"log/slog"
	"regexp"
	"strings"
)

// One rule per query shape; first match wins (a query matching two shapes gets
// the more specific earlier hint).
var routingRules = []struct {
	pattern *regexp.Regexp
	// shape names the query class for telemetry — the hint text is prose that
	// will be reworded; the shape is the stable key a follow-rate is measured
	// against.
	shape string
	hint  string
}{
	{
		// Ledger totals and sums: money/volume aggregates live in deal records,
		// not on any single page.
		regexp.MustCompile(`총액|총 ?금액|합계|견적 총`),
		"ledger-total",
		"금액 집계형 질의다 — 개별 페이지 스니펫이 아니라 deal_ledger(거래 원장)로 합산하라.",
	},
	{
		// Capacity/portfolio aggregates and superlatives: need enumeration over
		// the project roster, not ranked snippets.
		regexp.MustCompile(`총 ?용량|전체 ?(용량|규모)|(제일|가장) (큰|많은)|몇 (건|개소|MW)`),
		"aggregate",
		"집계·최상급 질의다 — 검색 상위 몇 건으론 못 답한다: wiki(action=\"index\")로 프로젝트를 전수 열람하거나 deal_ledger로 합산 후 답하라.",
	},
	{
		// Temporal mail questions: arrival times live in the mail archive, not
		// in analysis-page prose.
		regexp.MustCompile(`(마지막|최근)[^가-힣]*(메일|연락)|메일이? (온 게 )?언제`),
		"temporal-mail",
		"시간형 메일 질의다 — 위키 메일분석이 아니라 mail_archive로 실제 수신 시각을 확인하라.",
	},
	{
		// Relation enumeration: "everything connected to X" is a graph walk.
		regexp.MustCompile(`엮여? ?있|엮인|연결된 .*(전부|다|목록|뭐)`),
		"graph-relations",
		"관계 열거형 질의다 — 키워드 검색이 아니라 graphify(그래프 탐색)로 연결을 걸어라.",
	},
}

// routingHint returns a one-line Korean hint when the message matches a query
// shape page search cannot answer, else "". Pure and deterministic.
func routingHint(message string) string {
	hint, _ := routingHintWithShape(message)
	return hint
}

// routingHintWithShape also reports which rule fired, so callers can log the
// firing. Without it nothing in the journal said whether the hint ever
// triggers, let alone whether the model follows it — the hint has been a
// one-way nudge with no measurement since it landed.
func routingHintWithShape(message string) (hint, shape string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", ""
	}
	for _, r := range routingRules {
		if r.pattern.MatchString(message) {
			return "[회상 라우팅] " + r.hint, r.shape
		}
	}
	return "", ""
}

// appendRoutingHint attaches the hint to an already-emitted recall block. The
// hint sits AFTER the fence close tag: it is trusted server guidance, not
// recalled (untrusted) data, so it must not live inside the fence. Empty
// blocks stay empty — silent auto-recall turns must stay silent.
func appendRoutingHint(block, message string, logger *slog.Logger) string {
	if block == "" {
		return block
	}
	hint, shape := routingHintWithShape(message)
	if hint == "" {
		return block
	}
	if logger != nil {
		// Low volume by construction (only these four query shapes fire), and
		// the shape key is what a follow-rate is later measured against.
		logger.Info("recall routing hint", "shape", shape)
	}
	return block + "\n" + hint
}
