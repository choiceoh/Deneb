package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// kb_interview_suggest.go — proactive demand generation for the kb-interview
// skill (RSI P5 demand-generation axis). "Listened knowledge" — market,
// competitor, customer, pricing intelligence that lives ONLY in the operator's
// head — never enters the wiki unless someone is asked. Observed knowledge
// (projects, deals, people) accretes automatically from mail/data; listened
// knowledge does not. This task periodically checks a curated set of
// business-knowledge domains against wiki coverage and, when one is MISSING or
// STALE, posts a single workfeed question card offering to run an interview.
// Tapping the chip sends a chat message that naturally trips the kb-interview
// trigger, so the skill loads and the grilling begins.
//
// Opinionated defaults, operator-overridable: the built-in domain checklist is
// a solar-EPC-distribution starter. An optional wiki page
// (시스템/지식-도메인-체크리스트) lets the operator curate it without a rebuild —
// one "- key | label | kw1, kw2" line per domain.

const (
	kbInterviewSource        = "kb-interview-suggest"
	kbInterviewStateFile     = "kb-interview-suggest-state.json"
	kbInterviewInterval      = 24 * time.Hour
	kbInterviewCooldownDays  = 14  // per-domain re-suggest cooldown
	kbInterviewStaleDays     = 120 // a covered domain older than this is refresh-worthy
	kbInterviewChecklistPage = "시스템/지식-도메인-체크리스트"
)

// kbDomain is one knowledge domain the interview skill can fill.
type kbDomain struct {
	Key      string   // stable id for cooldown state
	Label    string   // operator-facing name
	Keywords []string // any match (title/path, lowercased) counts as coverage
}

// kbBuiltinDomains is the solar-EPC starter checklist. Keywords are matched
// case-insensitively against wiki page paths, so both the folder and the file
// name count. Deliberately small and high-value — a missing one is a real gap.
var kbBuiltinDomains = []kbDomain{
	{Key: "market-segmentation", Label: "시장 세분", Keywords: []string{"시장 세분", "시장세분", "세그먼트"}},
	{Key: "competitor-roster", Label: "경쟁사 로스터", Keywords: []string{"경쟁사", "경쟁 로스터", "competitor"}},
	{Key: "customer-profiles", Label: "핵심 고객 프로파일", Keywords: []string{"고객 프로파일", "핵심 고객", "고객군"}},
	{Key: "supplier-landscape", Label: "공급사/벤더 지형", Keywords: []string{"공급사", "벤더 지형", "제조사 선정", "조달 전략"}},
	{Key: "pricing-intel", Label: "단가/마진 인텔리전스", Keywords: []string{"단가", "마진", "가격 인텔", "pricing"}},
	{Key: "policy-regulatory", Label: "정책/규제 동향", Keywords: []string{"정책 동향", "규제 동향", "제도 변화", "전기사업법"}},
}

// kbCoverage is the per-domain evidence the detector reasons over.
type kbCoverage struct {
	Domain    kbDomain
	PageCount int
	NewestMs  int64 // 0 when PageCount == 0
}

// kbSuggestion is the chosen action for one cycle (nil = nothing to suggest).
type kbSuggestion struct {
	Domain kbDomain
	Reason string // "missing" | "stale"
	Detail string
}

// pickKBInterviewSuggestion is the pure decision core (unit-tested). Given each
// domain's coverage, the current time, and the per-domain last-suggested state,
// it picks at most ONE domain to surface: absent domains first (a gap costs the
// most), then the stalest covered domain past the freshness bar. Domains inside
// their cooldown window are skipped. Deterministic tie-break by domain key.
func pickKBInterviewSuggestion(cov []kbCoverage, now time.Time, lastSuggested map[string]int64) *kbSuggestion {
	cooldownMs := int64(kbInterviewCooldownDays) * 24 * 60 * 60 * 1000
	staleBeforeMs := now.Add(-time.Duration(kbInterviewStaleDays) * 24 * time.Hour).UnixMilli()
	nowMs := now.UnixMilli()

	var missing, stale *kbCoverage
	for i := range cov {
		c := &cov[i]
		if last, ok := lastSuggested[c.Domain.Key]; ok && nowMs-last < cooldownMs {
			continue // still cooling down
		}
		switch {
		case c.PageCount == 0:
			if missing == nil || c.Domain.Key < missing.Domain.Key {
				missing = c
			}
		case c.NewestMs < staleBeforeMs:
			// Stalest first; key tie-break keeps it deterministic.
			if stale == nil || c.NewestMs < stale.NewestMs ||
				(c.NewestMs == stale.NewestMs && c.Domain.Key < stale.Domain.Key) {
				stale = c
			}
		}
	}

	if missing != nil {
		return &kbSuggestion{
			Domain: missing.Domain,
			Reason: "missing",
			Detail: "위키에 이 도메인 페이지가 아직 없습니다.",
		}
	}
	if stale != nil {
		ageDays := (nowMs - stale.NewestMs) / (24 * 60 * 60 * 1000)
		return &kbSuggestion{
			Domain: stale.Domain,
			Reason: "stale",
			Detail: fmt.Sprintf("가장 최근 페이지가 %d일 전입니다 — 갱신 인터뷰가 필요할 수 있습니다.", ageDays),
		}
	}
	return nil
}

// kbInterviewState is the persisted per-domain cooldown ledger.
type kbInterviewState struct {
	Version       int              `json:"version"`
	LastSuggested map[string]int64 `json:"lastSuggested"` // domain key → unix millis
}

// kbInterviewSuggestTask is the periodic producer (server package so it can
// reach s.nativeWorkFeedStore() + s.wikiStore directly).
type kbInterviewSuggestTask struct {
	server    *Server
	statePath string
}

var _ autonomous.PeriodicTask = (*kbInterviewSuggestTask)(nil)

// registerKBInterviewSuggestTask wires the proactive interview-suggestion
// producer for the production state dir only (dev/live-test never posts).
func (s *Server) registerKBInterviewSuggestTask(homeDir string) {
	if s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_KB_INTERVIEW_SUGGEST_DISABLE") == "1" {
		s.logger.Info("kb-interview-suggest disabled via DENEB_KB_INTERVIEW_SUGGEST_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	task := &kbInterviewSuggestTask{
		server:    s,
		statePath: filepath.Join(stateDir, kbInterviewStateFile),
	}
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("kb-interview-suggest task registered", "interval", kbInterviewInterval.String())
}

func (t *kbInterviewSuggestTask) Name() string            { return "kb-interview-suggest" }
func (t *kbInterviewSuggestTask) Interval() time.Duration { return kbInterviewInterval }

func (t *kbInterviewSuggestTask) Run(ctx context.Context) error {
	s := t.server
	if s.wikiStore == nil {
		return nil
	}
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return nil
	}

	domains := t.resolveDomains()
	cov := t.coverageFor(domains)
	now := dentime.Now()

	state := t.loadState()
	pick := pickKBInterviewSuggestion(cov, now, state.LastSuggested)
	if pick == nil {
		return nil
	}

	// The chip's prompt is a message that trips the kb-interview skill trigger
	// ("지식 인터뷰") so the skill loads and starts grilling on this domain.
	chatPrompt := fmt.Sprintf("지식 인터뷰: '%s' 도메인을 인터뷰로 정리하자.", pick.Domain.Label)
	verb := "정리"
	if pick.Reason == "stale" {
		verb = "갱신"
	}
	item := workfeed.Item{
		Source:  kbInterviewSource,
		Title:   fmt.Sprintf("지식 인터뷰 제안: %s", pick.Domain.Label),
		Summary: fmt.Sprintf("%s — 인터뷰로 %s할까요?", pick.Detail, verb),
		Body: fmt.Sprintf(`머릿속에만 있는 업무 지식은 누가 물어보지 않으면 위키에 안 들어옵니다.

- 도메인: %s
- 상태: %s

아래 버튼을 누르면 이 도메인을 집요하게 질문해 위키 페이지로 %s합니다. 지금 말고 나중이면 무시하세요 — %d일 뒤 다시 제안합니다.`,
			pick.Domain.Label, pick.Detail, verb, kbInterviewCooldownDays),
		RefType:  "wiki-domain",
		RefID:    pick.Domain.Key,
		Metadata: map[string]string{"domain": pick.Domain.Key, "reason": pick.Reason},
		Status:   "unread",
		Question: true,
		Actions: []workfeed.Action{
			{
				ID:     "kbinterview:start",
				Kind:   workfeed.ActionAnswer,
				Label:  "인터뷰 시작",
				Prompt: chatPrompt,
			},
		},
	}
	if _, err := nf.Append(item); err != nil {
		s.logger.Warn("kb-interview-suggest: card append failed", "domain", pick.Domain.Key, "error", err)
		return nil
	}

	if state.LastSuggested == nil {
		state.LastSuggested = map[string]int64{}
	}
	state.LastSuggested[pick.Domain.Key] = now.UnixMilli()
	t.pruneState(&state, domains)
	t.saveState(state)
	s.logger.Info("kb-interview-suggest: posted interview suggestion card",
		"domain", pick.Domain.Key, "reason", pick.Reason)
	return nil
}

// coverageFor counts matching wiki pages per domain and records the newest
// page mtime. One ListPages walk feeds all domains.
func (t *kbInterviewSuggestTask) coverageFor(domains []kbDomain) []kbCoverage {
	pages, _ := t.server.wikiStore.ListPages("")
	dir := t.server.wikiStore.Dir()
	cov := make([]kbCoverage, len(domains))
	for i, d := range domains {
		cov[i].Domain = d
	}
	for _, p := range pages {
		lower := strings.ToLower(p)
		var mtime int64
		for i := range cov {
			if !pathMatchesDomain(lower, cov[i].Domain) {
				continue
			}
			if mtime == 0 {
				if info, err := os.Stat(filepath.Join(dir, p)); err == nil {
					mtime = info.ModTime().UnixMilli()
				}
			}
			cov[i].PageCount++
			if mtime > cov[i].NewestMs {
				cov[i].NewestMs = mtime
			}
		}
	}
	return cov
}

// kbSepStripper removes the separators wiki filenames use between words so a
// keyword matches regardless of spacing/hyphenation ("시장 세분" hits both
// "시장세분.md" and "한국-태양광-시장-세분.md").
var kbSepStripper = strings.NewReplacer(" ", "", "-", "", "_", "")

// pathMatchesDomain reports whether a lowercased wiki path matches any of the
// domain's keywords, comparing both raw and separator-stripped forms.
func pathMatchesDomain(lowerPath string, d kbDomain) bool {
	squashedPath := kbSepStripper.Replace(lowerPath)
	for _, kw := range d.Keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(lowerPath, kw) || strings.Contains(squashedPath, kbSepStripper.Replace(kw)) {
			return true
		}
	}
	return false
}

// resolveDomains returns the operator's curated checklist (시스템/지식-도메인-체크리스트
// wiki page, one "key | label | kw1, kw2" line per domain) when present, else
// the built-in starter set.
func (t *kbInterviewSuggestTask) resolveDomains() []kbDomain {
	page, err := t.server.wikiStore.ReadPage(kbInterviewChecklistPage + ".md")
	if err != nil || page == nil {
		return kbBuiltinDomains
	}
	parsed := parseKBChecklist(page.Body)
	if len(parsed) == 0 {
		return kbBuiltinDomains
	}
	return parsed
}

// parseKBChecklist reads "- key | label | kw1, kw2, kw3" bullet lines. Lines
// that don't fit are skipped, so surrounding prose is harmless.
func parseKBChecklist(body string) []kbDomain {
	var out []kbDomain
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		label := strings.TrimSpace(parts[1])
		if key == "" || label == "" {
			continue
		}
		var kws []string
		for _, kw := range strings.Split(parts[2], ",") {
			if kw = strings.TrimSpace(kw); kw != "" {
				kws = append(kws, kw)
			}
		}
		if len(kws) == 0 {
			continue
		}
		out = append(out, kbDomain{Key: key, Label: label, Keywords: kws})
	}
	return out
}

func (t *kbInterviewSuggestTask) loadState() kbInterviewState {
	st := kbInterviewState{LastSuggested: map[string]int64{}}
	raw, err := os.ReadFile(t.statePath)
	if err != nil {
		return st
	}
	var loaded kbInterviewState
	if json.Unmarshal(raw, &loaded) != nil || loaded.LastSuggested == nil {
		return st
	}
	return loaded
}

func (t *kbInterviewSuggestTask) saveState(st kbInterviewState) {
	st.Version = 1
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(t.statePath, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		t.server.logger.Warn("kb-interview-suggest: state save failed", "error", err)
	}
}

// pruneState drops cooldown entries for domains no longer in the checklist so a
// removed domain can't linger forever.
func (t *kbInterviewSuggestTask) pruneState(st *kbInterviewState, domains []kbDomain) {
	live := make(map[string]bool, len(domains))
	for _, d := range domains {
		live[d.Key] = true
	}
	for key := range st.LastSuggested {
		if !live[key] {
			delete(st.LastSuggested, key)
		}
	}
}
