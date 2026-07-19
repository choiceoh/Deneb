// heartbeat_research.go — deterministic new-data digest that seeds the
// heartbeat's self-directed research lane.
//
// Deneb's periodic background tasks are fixed-function: wiki-research (6h)
// refreshes individual project pages, the dreamer (8h) compresses diary/mail
// into wiki updates. Neither formulates NEW questions from what accumulated
// across sources. This file adds that missing trigger without any LLM call of
// its own: a cheap filesystem scan counts what arrived since the last nudge
// (mail-analysis pages per project, other wiki writes), and once enough piled
// up, the next heartbeat turn is asked to look for cross-cutting patterns and
// queue "[연구] ..." items into HEARTBEAT.md — which the following ticks then
// execute with the full toolset. Judgment stays inside the heartbeat turn;
// this scan only decides WHEN it is worth asking. Cost bounds: fires at most
// once per researchNudgeMinInterval, and queued items are capped at two with
// a finish-within-1-2-ticks discipline (the existing 3-strikes archive rule
// is the backstop against a stalled item burning every tick).
package heartbeat

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// researchNudgeMinAnalyses is the minimum number of new mail-analysis pages
	// (across all projects) required to fire a nudge — a single routine mail is
	// not worth a dedicated cross-data research pass.
	researchNudgeMinAnalyses = 2
	// researchNudgeMinInterval throttles the lane to roughly once a day; 20h
	// instead of 24h so the firing hour can drift earlier with the data flow
	// instead of creeping later every day.
	researchNudgeMinInterval = 20 * time.Hour
	// researchNudgeMaxWindow caps the scan window after downtime so a week-old
	// backlog does not produce a misleading "지난 N시간 유입" digest.
	researchNudgeMaxWindow = 72 * time.Hour
)

// researchNudgeState persists the last firing time under the state dir
// (~/.deneb/heartbeat-research.json).
type researchNudgeState struct {
	LastNudgeAt time.Time `json:"lastNudgeAt"`
}

func (t *heartbeatTask) researchStatePath() string {
	return filepath.Join(t.homeDir, ".deneb", "heartbeat-research.json")
}

// detectResearchNudge scans the wiki for data accumulated since the last nudge
// and returns the research-lane trigger text, or "" when quiet. It fires at
// most once per researchNudgeMinInterval and persists the marker immediately
// when firing — a failed agent turn must not re-fire every 30 minutes.
func (t *heartbeatTask) detectResearchNudge(now time.Time) string {
	if t.homeDir == "" {
		return ""
	}
	statePath := t.researchStatePath()
	st := loadResearchNudgeState(statePath)
	if st.LastNudgeAt.After(now) {
		// Clock skew or a corrupted marker: a future timestamp would silently
		// mute the lane until the wall clock catches up. Treat as unset.
		st.LastNudgeAt = time.Time{}
	}
	if !st.LastNudgeAt.IsZero() && now.Sub(st.LastNudgeAt) < researchNudgeMinInterval {
		return ""
	}
	since := now.Add(-researchNudgeMaxWindow)
	if st.LastNudgeAt.After(since) {
		since = st.LastNudgeAt
	}
	dig := scanWikiNewData(filepath.Join(t.homeDir, ".deneb", "wiki"), since)
	if dig.totalAnalyses() < researchNudgeMinAnalyses {
		return ""
	}
	if err := saveResearchNudgeState(statePath, researchNudgeState{LastNudgeAt: now}); err != nil {
		// Fail closed: without a persisted marker every subsequent tick would
		// re-fire the nudge (and its cloud turn) until the write recovers.
		t.logger.Warn("heartbeat: research nudge state save failed, skipping nudge", "error", err)
		return ""
	}
	t.logger.Info("heartbeat: research nudge fired",
		"analyses", dig.totalAnalyses(), "wikiUpdates", dig.wikiUpdates,
		"window", now.Sub(since).Round(time.Hour))
	return buildResearchNudge(dig, now.Sub(since))
}

// wikiNewDataDigest is the deterministic count of wiki writes inside the scan
// window, split into mail-analysis inflow (the substantive 업무 signal) and
// everything else.
type wikiNewDataDigest struct {
	analysesByProject map[string]int // project name → new mail-analysis pages ("" = 미분류)
	wikiUpdates       int            // other .md writes (dreamer/research/manual)
}

func (d wikiNewDataDigest) totalAnalyses() int {
	n := 0
	for _, c := range d.analysesByProject {
		n += c
	}
	return n
}

// scanWikiNewData counts wiki .md files modified after since. Best-effort:
// unreadable subtrees are skipped and .git is excluded. Mail-analysis pages
// are recognized in both the current layout (프로젝트/<이름>/메일분석/) and the
// transition-era buckets (프로젝트/메일분석/, 프로젝트/mail-analyses/ → 미분류).
func scanWikiNewData(wikiDir string, since time.Time) wikiNewDataDigest {
	dig := wikiNewDataDigest{analysesByProject: map[string]int{}}
	_ = filepath.WalkDir(wikiDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // best-effort scan: returning the error would abort the whole walk
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || !info.ModTime().After(since) {
			//nolint:nilerr // best-effort scan: skip unreadable entries, keep walking
			return nil
		}
		rel, rerr := filepath.Rel(wikiDir, path)
		if rerr != nil {
			//nolint:nilerr // best-effort scan: skip unresolvable paths, keep walking
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if project, ok := mailAnalysisProject(parts); ok {
			dig.analysesByProject[project]++
			return nil
		}
		dig.wikiUpdates++
		return nil
	})
	return dig
}

// mailAnalysisProject reports whether the relative wiki path (split on "/") is
// a mail-analysis page, and which project owns it ("" = unfiled bucket).
func mailAnalysisProject(parts []string) (string, bool) {
	if len(parts) < 3 || parts[0] != "프로젝트" {
		return "", false
	}
	dir := parts[len(parts)-2]
	if dir != "메일분석" && dir != "mail-analyses" {
		return "", false
	}
	if len(parts) == 3 {
		return "", true // 프로젝트/메일분석/x.md — unfiled bucket
	}
	return parts[1], true
}

// buildResearchNudge renders the trigger block the heartbeat turn receives.
// It carries the per-project inflow summary plus the self-queue contract:
// formalize at most two "[연구]" items via heartbeat_update, finish each
// within a tick or two, land insights in the wiki, and stay silent unless the
// finding needs the operator's judgment.
func buildResearchNudge(dig wikiNewDataDigest, window time.Duration) string {
	type projCount struct {
		name  string
		count int
	}
	projs := make([]projCount, 0, len(dig.analysesByProject))
	for name, c := range dig.analysesByProject {
		projs = append(projs, projCount{name, c})
	}
	sort.Slice(projs, func(i, j int) bool {
		if projs[i].count != projs[j].count {
			return projs[i].count > projs[j].count
		}
		return projs[i].name < projs[j].name
	})
	parts := make([]string, 0, len(projs))
	for _, p := range projs {
		name := p.name
		if name == "" {
			name = "미분류"
		}
		parts = append(parts, fmt.Sprintf("%s %d", name, p.count))
	}
	summary := fmt.Sprintf("메일분석 %d건(%s)", dig.totalAnalyses(), strings.Join(parts, " · "))
	if dig.wikiUpdates > 0 {
		summary += fmt.Sprintf(" · 기타 위키 갱신 %d건", dig.wikiUpdates)
	}
	hours := int(window.Hours())
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf(`[리서치 레인 — 자동 데이터 다이제스트] 지난 %d시간 신규 유입: %s.
이 축적분을 훑고, 단건 메일분석 카드로는 안 보이는 교차 패턴·추세·리스크(같은 이슈의 반복, 조건 변화, 지연 징후, 거래처 간 연결)를 찾으세요:
1) 조사할 가치가 있으면 질문을 정식화해 heartbeat_update로 HEARTBEAT.md에 "[연구] ..." 항목으로 등록하세요. 활성 [연구] 항목은 최대 2개 — 이미 2개 있으면 새로 등록하지 마세요.
2) 등록한 항목은 다음 1~2회 점검 안에 조사(위키·메일 아카이브·웹)를 끝내고, 인사이트를 위키(관련 프로젝트의 로그.md 또는 대표페이지 현재 상태)에 기록한 뒤 항목을 제거하세요. 며칠씩 걸어두지 마세요.
3) 사용자 보고는 임원 판단이 필요한 발견일 때만 하세요. 정리 수준의 결과는 위키 기록으로 충분합니다(NO_REPLY).
패턴이 없으면 아무것도 등록하지 말고 NO_REPLY 하세요.`, hours, summary)
}

func loadResearchNudgeState(path string) researchNudgeState {
	var st researchNudgeState
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func saveResearchNudgeState(path string, st researchNudgeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
