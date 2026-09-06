// source_check.go — Verify one claim against the open web and return a verdict
// with quoted evidence. Clean-room after the pi-web-access review (2026-09-06).
//
// Deneb already had every part — search, candidate ranking, fetch, query-focused
// excerpting, the reranker — but no tool that turns "is this true?" into ONE
// artifact. The main model would search, open pages and reason in prose, and the
// answer's evidence was whatever it remembered to mention. This tool makes the
// evidence the output: each source carries a stance and a verbatim quote (with
// its offset in the fetched text and a content hash, so a citation can be
// re-checked), and the verdict is computed from the stances by a fixed rule —
// not by the judge model's mood.
package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// SourceVerdict is the fixed-rule outcome of a source check.
type SourceVerdict string

const (
	VerdictSupported       SourceVerdict = "supported"
	VerdictContradicted    SourceVerdict = "contradicted"
	VerdictUnclear         SourceVerdict = "unclear"
	VerdictMissingEvidence SourceVerdict = "missing-evidence"
)

const (
	sourceCheckMaxQueries   = 3
	sourceCheckMaxFetch     = 5
	sourceCheckDefaultFetch = 3
	sourceCheckSearchCount  = 10
	sourceCheckCandidates   = 20
	sourceCheckPageChars    = 12000
	sourceCheckExcerptChars = 1500
	// Roomy on purpose: the lightweight role may be a cloud twin that ignores
	// thinking-off shaping and spends the budget reasoning first (live run
	// 2026-09-06: 900 tokens came back as truncated JSON).
	sourceCheckJudgeTokens         = 2048
	sourceCheckJudgeFallbackTokens = 1200
)

// SourceCheckInput is the tool's argument shape.
type SourceCheckInput struct {
	Claim   string   `json:"claim"`
	Queries []string `json:"queries,omitempty"`
	Fetch   int      `json:"fetch,omitempty"`
	Domains []string `json:"domains,omitempty"`
}

// SourceCheckSource is one read source with the judge's stance on the claim.
type SourceCheckSource struct {
	URL       string  `json:"url"`
	Title     string  `json:"title,omitempty"`
	Stance    string  `json:"stance"` // supports | contradicts | unclear
	Quote     string  `json:"quote,omitempty"`
	Verbatim  bool    `json:"verbatim"`
	Offset    int     `json:"offset"` // byte offset of Quote in the fetched body; -1 when not verbatim
	Reason    string  `json:"reason,omitempty"`
	SHA256    string  `json:"sha256"`
	Relevance float64 `json:"relevance,omitempty"`
	excerpt   string
	body      string
}

// SourceCheckResult is the artifact the tool returns (also rendered as text).
type SourceCheckResult struct {
	Claim      string              `json:"claim"`
	Verdict    SourceVerdict       `json:"verdict"`
	Confidence float64             `json:"confidence"`
	Summary    string              `json:"summary,omitempty"`
	Sources    []SourceCheckSource `json:"sources"`
	Searched   int                 `json:"searched"`
	Fetched    int                 `json:"fetched"`
	Notes      []string            `json:"notes,omitempty"`
}

// Seams for deterministic tests — the live wiring is the package's own
// search, fetch and lightweight-role LLM call.
var (
	sourceSearchFn                      = webSearchWithURLs
	sourceFetchFn  urlFetchDetailedFunc = webFetchURLDetailed
	sourceJudgeFn                       = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		return pilot.CallLocalLLM(ctx, system, user, maxTokens)
	}
	// The tiny role is a no-think model by configuration, so when the primary
	// judge answers with reasoning instead of JSON (or nothing), this one
	// answers the same prompt plainly.
	sourceJudgeFallbackFn = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		return pilot.CallTinyLLM(ctx, system, user, maxTokens)
	}
)

// ToolSourceCheck returns the source_check tool function.
func ToolSourceCheck(cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in SourceCheckInput
		if err := jsonutil.UnmarshalInto("source_check params", input, &in); err != nil {
			return "", err
		}
		in.Claim = strings.TrimSpace(in.Claim)
		if in.Claim == "" {
			return "", fmt.Errorf("claim이 필요합니다 (검증할 주장 한 문장)")
		}
		res := runSourceCheck(ctx, cache, localAI, spill, in)
		return renderSourceCheck(res), nil
	}
}

func runSourceCheck(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, in SourceCheckInput) SourceCheckResult {
	res := SourceCheckResult{Claim: in.Claim, Verdict: VerdictMissingEvidence}
	fetchN := in.Fetch
	if fetchN <= 0 {
		fetchN = sourceCheckDefaultFetch
	}
	if fetchN > sourceCheckMaxFetch {
		fetchN = sourceCheckMaxFetch
	}

	// 1. Search: the claim itself, plus any caller queries (capped).
	queries := []string{in.Claim}
	for _, q := range in.Queries {
		if q = strings.TrimSpace(q); q != "" && q != in.Claim {
			queries = append(queries, q)
		}
		if len(queries) >= sourceCheckMaxQueries {
			break
		}
	}
	var organic []searchResult
	var answerLink, knowledgeLink string
	seen := map[string]bool{}
	for _, q := range queries {
		_, results, al, kl, err := sourceSearchFn(ctx, q, sourceCheckSearchCount)
		res.Searched++
		if err != nil {
			res.Notes = append(res.Notes, "검색 실패("+truncateForNote(q)+"): "+err.Error())
			continue
		}
		if answerLink == "" {
			answerLink, knowledgeLink = al, kl
		}
		for _, r := range results {
			if r.URL == "" || seen[r.URL] || !allowedDomain(r.URL, in.Domains) {
				continue
			}
			seen[r.URL] = true
			organic = append(organic, r)
		}
	}
	if len(organic) == 0 {
		res.Notes = append(res.Notes, "검색 결과 없음")
		return res
	}
	titles := map[string]string{}
	for _, r := range organic {
		titles[r.URL] = r.Title
	}

	// 2. Rank candidates the way search+fetch does (answer box, snippet overlap,
	//    host diversity, denylist), then read the top pages that are actually
	//    usable — thin and blocked pages are skipped, not counted as evidence.
	ranked, _ := rankFetchCandidates(in.Claim, answerLink, knowledgeLink, organic, sourceCheckCandidates)
	if len(in.Domains) > 0 {
		kept := ranked[:0]
		for _, u := range ranked {
			if allowedDomain(u, in.Domains) {
				kept = append(kept, u)
			}
		}
		ranked = kept
	}
	pages := fillUsableFetches(ctx, cache, localAI, spill, ranked, fetchN, sourceCheckPageChars, in.Claim, sourceFetchFn)
	res.Fetched = len(pages)
	if len(pages) == 0 {
		res.Notes = append(res.Notes, "읽을 수 있는 페이지 없음 (후보 "+fmt.Sprint(len(ranked))+")")
		return res
	}

	// 3. Excerpt each page around the claim; hash the body so a quote can be
	//    re-checked against exactly what was read.
	for _, p := range pages {
		body := fetchResultBody(p.content)
		excerpt := body
		if fr, ok := focusExcerpt(body, in.Claim, sourceCheckExcerptChars); ok && strings.TrimSpace(fr.Text) != "" {
			excerpt = fr.Text
		} else if len([]rune(excerpt)) > sourceCheckExcerptChars {
			excerpt = string([]rune(excerpt)[:sourceCheckExcerptChars])
		}
		sum := sha256.Sum256([]byte(body))
		res.Sources = append(res.Sources, SourceCheckSource{
			URL: p.url, Title: titles[p.url], Stance: "unclear", Offset: -1,
			SHA256: hex.EncodeToString(sum[:]), excerpt: excerpt, body: body,
		})
	}

	// 4. Reranker (when resident): order sources by relevance to the claim so the
	//    judge reads the strongest first. Absent or failing → search order.
	if rr := currentSearchReranker(); rr != nil {
		docs := make([]string, len(res.Sources))
		for i, s := range res.Sources {
			docs[i] = s.excerpt
		}
		if scores, err := rr.Rerank(ctx, in.Claim, docs); err == nil && len(scores) == len(docs) {
			for i := range res.Sources {
				res.Sources[i].Relevance = scores[i]
			}
			sort.SliceStable(res.Sources, func(a, b int) bool { return res.Sources[a].Relevance > res.Sources[b].Relevance })
		}
	}

	// 5. Judge: one call over all excerpts. Stances come from the model; the
	//    verdict does not (see aggregateVerdict).
	summary, note, err := judgeSources(ctx, in.Claim, res.Sources)
	if note != "" {
		res.Notes = append(res.Notes, note)
	}
	if err != nil {
		res.Notes = append(res.Notes, "판정 모델 실패 — 발췌만 반환: "+err.Error())
	}
	res.Verdict, res.Confidence = aggregateVerdict(res.Sources)
	if note != "" && res.Confidence > 0 {
		// A fallback judge is a weaker reader; its verdict must not carry the
		// primary judge's confidence.
		res.Confidence = math.Min(res.Confidence*0.75, 0.6)
	}
	res.Summary = defaultSummary(res)
	if summary != "" {
		res.Summary += " — " + summary
	}
	return res
}

// allowedDomain reports whether rawURL's host is one of domains (or a subdomain
// of one). An empty list allows everything.
func allowedDomain(rawURL string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range domains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "."))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

const sourceJudgeSystem = "너는 주장 검증기다. 주장은 **사실 명제**로 읽는다. 번호가 붙은 출처 발췌를 받아 출처마다 stance를 정한다: " +
	"supports = 발췌가 그 사실을 긍정한다 · contradicts = 발췌가 그 사실이 틀렸다고 명시한다 · unclear = 그 외 전부. " +
	"주장의 법적·규범적 지위를 논하거나(예: '관습헌법인가'), 표현을 비판하거나, 관련 주제만 다루는 출처는 사실을 반박하는 것이 아니다 — unclear로 둔다. " +
	"quote는 발췌에서 근거 문장을 **한 글자도 바꾸지 않고**(공백·따옴표 포함) 그대로 복사한다; 제목만 있는 출처는 quote를 비운다. " +
	"발췌에 없는 말을 지어내지 마라. 출력은 JSON 하나만: " +
	`{"sources":[{"n":1,"stance":"supports|contradicts|unclear","quote":"…","reason":"한 문장"}],"summary":"한 줄 요약"}`

type judgeReply struct {
	Sources []struct {
		N      int    `json:"n"`
		Stance string `json:"stance"`
		Quote  string `json:"quote"`
		Reason string `json:"reason"`
	} `json:"sources"`
	Summary string `json:"summary"`
}

// judgeSources asks the judge for per-source stances. Returns the judge's
// one-line summary, a note when the fallback judge had to answer, and an error
// only when neither judge produced parseable JSON (stances then stay unclear).
func judgeSources(ctx context.Context, claim string, sources []SourceCheckSource) (summary, note string, err error) {
	if len(sources) == 0 {
		return "", "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "주장: %s\n\n", claim)
	for i, s := range sources {
		fmt.Fprintf(&b, "[%d] %s\n%s\n\n", i+1, s.URL, s.excerpt)
	}
	user := b.String()
	reply, perr := parseJudgeReply(sourceJudgeFn(ctx, sourceJudgeSystem, user, sourceCheckJudgeTokens))
	if perr != nil {
		// Primary judge reasoned itself out of budget or returned prose — the
		// no-think tiny role gets the same prompt before we give up.
		reply, err = parseJudgeReply(sourceJudgeFallbackFn(ctx, sourceJudgeSystem, user, sourceCheckJudgeFallbackTokens))
		if err != nil {
			return "", "", fmt.Errorf("primary: %w; fallback: %w", perr, err)
		}
		note = "판정: 1차 모델 응답 불량(" + perr.Error() + ") → tiny 롤 폴백으로 판정"
	}
	for _, js := range reply.Sources {
		i := js.N - 1
		if i < 0 || i >= len(sources) {
			continue
		}
		s := &sources[i]
		s.Stance = normalizeStance(js.Stance)
		s.Reason = strings.TrimSpace(js.Reason)
		s.Quote = strings.TrimSpace(js.Quote)
		s.Offset = -1
		if s.Quote != "" {
			// Verbatim means the quote is findable in what was actually read —
			// the difference between a citation and a paraphrase.
			if idx := locateQuote(s.body, s.Quote); idx >= 0 {
				s.Offset = idx
				s.Verbatim = true
			}
		}
	}
	return strings.TrimSpace(reply.Summary), note, nil
}

// parseJudgeReply turns a raw judge answer into the reply struct, treating a
// blank answer and non-JSON alike as "no verdict from this judge".
func parseJudgeReply(raw string, callErr error) (judgeReply, error) {
	var reply judgeReply
	if callErr != nil {
		return reply, callErr
	}
	if strings.TrimSpace(raw) == "" {
		return reply, fmt.Errorf("빈 응답")
	}
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), &reply); err != nil {
		return reply, fmt.Errorf("판정 응답이 JSON이 아님: %w", err)
	}
	return reply, nil
}

// locateQuote finds quote in body and returns its byte offset, or -1. Judges
// routinely collapse the whitespace of what they copy (a line break inside a
// sentence becomes a space), so each whitespace run in the quote matches any
// whitespace run in the body — the offset still points at the real text.
func locateQuote(body, quote string) int {
	parts := strings.Fields(quote)
	if len(parts) == 0 {
		return -1 // a blank "quote" would match the first whitespace run
	}
	if idx := strings.Index(body, quote); idx >= 0 {
		return idx
	}
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile(strings.Join(parts, `\s+`))
	if err != nil {
		return -1
	}
	if loc := re.FindStringIndex(body); loc != nil {
		return loc[0]
	}
	return -1
}

func normalizeStance(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "supports", "support", "supported", "지지":
		return "supports"
	case "contradicts", "contradict", "contradicted", "반박":
		return "contradicts"
	default:
		return "unclear"
	}
}

// aggregateVerdict is the fixed rule: only supporting stances → supported; only
// contradicting → contradicted; both → unclear (the sources disagree); none →
// missing-evidence. Confidence grows with agreeing sources and stops short of
// certainty — three agreeing web pages are still three web pages.
func aggregateVerdict(sources []SourceCheckSource) (SourceVerdict, float64) {
	var sup, con int
	for _, s := range sources {
		switch s.Stance {
		case "supports":
			sup++
		case "contradicts":
			con++
		}
	}
	conf := func(n int) float64 {
		c := 0.5 + 0.15*float64(n)
		if c > 0.95 {
			c = 0.95
		}
		return c
	}
	switch {
	case sup > 0 && con == 0:
		return VerdictSupported, conf(sup)
	case con > 0 && sup == 0:
		return VerdictContradicted, conf(con)
	case sup > 0 && con > 0:
		return VerdictUnclear, 0.3
	default:
		return VerdictMissingEvidence, 0
	}
}

func defaultSummary(r SourceCheckResult) string {
	switch r.Verdict {
	case VerdictSupported:
		return fmt.Sprintf("지지하는 출처 %d건, 반박 없음", countStance(r.Sources, "supports"))
	case VerdictContradicted:
		return fmt.Sprintf("반박하는 출처 %d건, 지지 없음", countStance(r.Sources, "contradicts"))
	case VerdictUnclear:
		return fmt.Sprintf("출처가 엇갈림 — 지지 %d건 · 반박 %d건", countStance(r.Sources, "supports"), countStance(r.Sources, "contradicts"))
	default:
		if r.Fetched == 0 {
			return "읽을 수 있는 출처가 없음"
		}
		return fmt.Sprintf("읽은 %d페이지 어디에도 판단 근거 없음", r.Fetched)
	}
}

func countStance(sources []SourceCheckSource, stance string) int {
	n := 0
	for _, s := range sources {
		if s.Stance == stance {
			n++
		}
	}
	return n
}

// extractJSONObject returns the first {...} block in s (models wrap JSON in
// prose or fences more often than not), or s itself when there is none.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

func truncateForNote(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// renderSourceCheck writes the human-readable verdict first (what the agent
// relays) and the JSON artifact after it (what the agent quotes from).
func renderSourceCheck(r SourceCheckResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 주장 검증: %s\n", r.Claim)
	fmt.Fprintf(&b, "판정: **%s** (신뢰 %.2f) — %s\n", r.Verdict, r.Confidence, r.Summary)
	fmt.Fprintf(&b, "검색 %d회 · 읽음 %d페이지 · 근거 %d건\n", r.Searched, r.Fetched, len(r.Sources))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "- 참고: %s\n", n)
	}
	for i, s := range r.Sources {
		title := s.Title
		if title == "" {
			title = s.URL
		}
		fmt.Fprintf(&b, "\n%d. [%s] %s — %s\n", i+1, s.Stance, title, s.URL)
		if s.Quote != "" {
			mark := "인용(원문 그대로, offset %d)"
			if !s.Verbatim {
				mark = "인용(원문에서 못 찾음 — 의역 가능성, offset %d)"
			}
			fmt.Fprintf(&b, "   "+mark+": \"%s\"\n", s.Offset, s.Quote)
		}
		if s.Reason != "" {
			fmt.Fprintf(&b, "   이유: %s\n", s.Reason)
		}
		fmt.Fprintf(&b, "   sha256: %s\n", s.SHA256[:12])
	}
	// The artifact: stable field names the agent can cite from.
	if data, err := json.MarshalIndent(r, "", " "); err == nil {
		b.WriteString("\n```json\n")
		b.Write(data)
		b.WriteString("\n```\n")
	}
	return b.String()
}
