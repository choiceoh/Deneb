// project_digest.go — the dream cycle's per-project latest-progress roll-up.
//
// The wiki holds project STATE (a page's summary describes the whole page;
// `updated` is just the last-touched date). Neither answers "what actually moved
// on this project recently" — the chief-of-staff's glance question. Each dream
// cycle reads the same fresh diary/MEMORY input the synthesis pass consumes and
// asks the LLM to roll it up BY PROJECT, then writes each roll-up into that
// project's 대표페이지 "## 현재 상태" section (see project_status.go). The 모아보기
// screen reads those sections; mail analysis keeps them fresh between cycles by
// appending dated bullets (the server's mail sink → AppendProjectStatusLine).
//
// Like open_loops.go this is a separate, focused LLM call on purpose: the
// wiki-synthesis JSON contract has a history of drift-induced parse failures,
// and a failed digest pass must never cost a wiki consolidation cycle (and vice
// versa). Best-effort, fail-open — a bad pass logs and is skipped. The roll-up is
// anchored to the real 프로젝트/ taxonomy: a project label the LLM invents that
// isn't a real page is dropped, so it can't write a stray page or a dead card.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// projectCategoryPrefix is the top-level wiki category that holds project pages
// (프로젝트/<name>.md). Digests are anchored to these direct pages so a digest's
// project label always names a real, navigable bucket — not free LLM text.
const projectCategoryPrefix = "프로젝트"

// ProjectDigest is one project's latest-progress roll-up for the cycle. Path is
// resolved from the digest's project label against the real taxonomy (not from
// the LLM) and names the 대표페이지 the roll-up is written to.
type ProjectDigest struct {
	Project  string   `json:"project"`       // project name (must match a known 프로젝트/<name> page)
	Headline string   `json:"headline"`      // one-line current status (Korean)
	Bullets  []string `json:"bullets"`       // 2-3 concrete recent developments
	Due      string   `json:"due,omitempty"` // YYYY-MM-DD imminent deadline when stated, else ""
	Path     string   `json:"-"`             // resolved 대표페이지 path (filled by extractProjectDigests)
}

// projectDigestMaxPerCycle caps extraction output; a cycle that "finds" dozens
// of active projects is hallucinating, not summarizing.
const projectDigestMaxPerCycle = 12

// projectDigestMaxBullets bounds each project's bullet list — a glance card, not
// a full log.
const projectDigestMaxBullets = 3

// projectDigestMaxTokens bounds the extraction response.
const projectDigestMaxTokens = 2048

// projectDigestTimeout bounds the extraction LLM call so a wedged backend costs
// the digest pass, not the remaining dream-cycle budget.
const projectDigestTimeout = 2 * time.Minute

// extractProjectDigests runs the focused per-project roll-up over the cycle
// input, anchored to the real 프로젝트/ taxonomy so a hallucinated or misspelled
// project label can't produce a dead drill-down or a stray page. Each kept
// digest carries the resolved 대표페이지 Path.
func (wd *WikiDreamer) extractProjectDigests(ctx context.Context, content string) ([]ProjectDigest, error) {
	if wd.client == nil || wd.store == nil || strings.TrimSpace(content) == "" {
		return nil, nil
	}
	// Anchor to the real project pages; no projects → nothing to digest.
	known := wd.store.knownProjects()
	if len(known) == 0 {
		return nil, nil
	}
	byName := make(map[string]string, len(known)) // name → 대표페이지 path
	names := make([]string, 0, len(known))
	for _, r := range known {
		byName[r.Name] = r.Path
		names = append(names, r.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, projectDigestTimeout)
	defer cancel()

	prompt := fmt.Sprintf(`아래 일지/메모에서 **프로젝트별 최신 진행상황**만 요약하세요.

## 프로젝트 목록 (project 값은 반드시 이 목록에서 정확히 그대로 고르세요)
%s
목록에 없는 프로젝트는 만들지 말고 제외하세요.

## 기준
- 최근에 실제로 진행·변화가 있었던 프로젝트만 (이름만 스쳐 지나가고 변화가 없으면 제외)
- 각 프로젝트마다: 한 줄 헤드라인 + 핵심 변화 2~3개 (불릿, 명사형 간결하게)
- 임박한 마감·약속이 있으면 due 에 날짜 (없으면 생략)
- 잡담·감상·무관한 내용 제외, 최대 %d개, 확신 있는 것만

## 출력 (JSON 배열만, 다른 텍스트 없이)
[{"project":"프로젝트명","headline":"한 줄 현황(한국어)","bullets":["변화1","변화2"],"due":"YYYY-MM-DD(있으면)"}]
근황이 없으면 [] 를 반환하세요.

## 일지/메모
%s`, formatProjectList(names), projectDigestMaxPerCycle, content)

	systemJSON, _ := json.Marshal("You summarize the latest progress per project. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: projectDigestMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("project-digest LLM call: %w", err)
	}
	digests, err := parseProjectDigests(resp)
	if err != nil {
		return nil, err
	}
	// Hard guard: keep only digests whose project is a real page, regardless of
	// what the model emitted, and attach its resolved path.
	kept := make([]ProjectDigest, 0, len(digests))
	for _, d := range digests {
		path, ok := byName[strings.TrimSpace(d.Project)]
		if !ok {
			continue
		}
		d.Path = path
		kept = append(kept, d)
	}
	if dropped := len(digests) - len(kept); dropped > 0 {
		wd.logger.Debug("wiki-dream: dropped digests for unknown projects", "dropped", dropped)
	}
	return kept, nil
}

// applyProjectDigests writes each roll-up into its project's 현재 상태 section
// (headline first, then bullets). Returns how many pages were updated. now is
// injected for deterministic tests. Best-effort: a per-page write failure logs
// and is skipped, never aborting the others.
func (wd *WikiDreamer) applyProjectDigests(digests []ProjectDigest, now time.Time) int {
	wrote := 0
	for _, d := range digests {
		if d.Path == "" {
			continue
		}
		lines := make([]string, 0, len(d.Bullets)+1)
		if h := strings.TrimSpace(d.Headline); h != "" {
			lines = append(lines, h)
		}
		for _, b := range d.Bullets {
			if b = strings.TrimSpace(b); b != "" {
				lines = append(lines, b)
			}
		}
		if len(lines) == 0 {
			continue
		}
		if err := wd.store.SetProjectStatus(d.Path, lines, d.Due, now); err != nil {
			wd.logger.Warn("wiki-dream: project status write failed", "path", d.Path, "error", err)
			continue
		}
		wrote++
	}
	return wrote
}

// formatProjectList renders the known project names as a bulleted list for the
// extraction prompt.
func formatProjectList(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("- ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseProjectDigests decodes the extraction response: fences stripped, capped,
// empty entries dropped, bullets bounded, free text redacted.
func parseProjectDigests(text string) ([]ProjectDigest, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	if text == "" {
		return nil, nil
	}
	var digests []ProjectDigest
	if err := json.Unmarshal([]byte(text), &digests); err != nil {
		return nil, fmt.Errorf("parse project digests: %w (raw: %.200s)", err, text)
	}
	out := digests[:0]
	for _, d := range digests {
		d.Project = strings.TrimSpace(redact.String(d.Project))
		d.Headline = strings.TrimSpace(redact.String(d.Headline))
		d.Due = strings.TrimSpace(d.Due)
		// A digest needs both an owning project and something to say.
		if d.Project == "" || d.Headline == "" {
			continue
		}
		bullets := make([]string, 0, len(d.Bullets))
		for _, b := range d.Bullets {
			b = strings.TrimSpace(redact.String(b))
			if b == "" {
				continue
			}
			bullets = append(bullets, b)
			if len(bullets) >= projectDigestMaxBullets {
				break
			}
		}
		d.Bullets = bullets
		out = append(out, d)
		if len(out) >= projectDigestMaxPerCycle {
			break
		}
	}
	return out, nil
}
