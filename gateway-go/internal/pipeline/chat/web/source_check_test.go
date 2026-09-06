package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func sourceEnvelope(body string) string {
	return "<metadata>\nSignals: serper_scrape\n</metadata>\n<content>\n" + body + "\n</content>"
}

// stubSourceCheck wires the three seams to canned pages and a canned judge, and
// restores them after the test.
func stubSourceCheck(t *testing.T, pages map[string]string, judge func(user string) (string, error)) {
	t.Helper()
	origSearch, origFetch, origJudge, origFallback, origRerank := sourceSearchFn, sourceFetchFn, sourceJudgeFn, sourceJudgeFallbackFn, currentSearchReranker()
	t.Cleanup(func() {
		sourceSearchFn, sourceFetchFn, sourceJudgeFn, sourceJudgeFallbackFn = origSearch, origFetch, origJudge, origFallback
		SetSearchReranker(origRerank)
	})
	SetSearchReranker(nil)
	// Tests that want the fallback set it explicitly; by default it fails too.
	sourceJudgeFallbackFn = func(context.Context, string, string, int) (string, error) {
		return "", errors.New("fallback not wired")
	}
	sourceSearchFn = func(_ context.Context, q string, _ int) (string, []searchResult, string, string, error) {
		var out []searchResult
		for u := range pages {
			out = append(out, searchResult{Title: "page " + u, URL: u, Description: "snippet about the claim"})
		}
		return "", out, "", "", nil
	}
	sourceFetchFn = func(_ context.Context, _ *FetchCache, _ *LocalAIExtractor, _ tooldeps.SpilloverStore, u string, _ int, _ string) (fetchOutcome, error) {
		body, ok := pages[u]
		if !ok {
			return fetchOutcome{}, errors.New("unknown page")
		}
		env := sourceEnvelope(body)
		return fetchOutcome{Content: env, Assess: assessFetchResult(env, nil)}, nil
	}
	sourceJudgeFn = func(_ context.Context, _ string, user string, _ int) (string, error) { return judge(user) }
}

func longBody(sentence string) string {
	return strings.Repeat("배경 설명 문단입니다. ", 30) + sentence + " " + strings.Repeat("이어지는 설명입니다. ", 30)
}

// The judge sees numbered sources in search order; answer by URL so the test does
// not depend on map iteration order.
func judgeByURL(user string, stance map[string]string, quote map[string]string) (string, error) {
	type src struct {
		N      int    `json:"n"`
		Stance string `json:"stance"`
		Quote  string `json:"quote"`
		Reason string `json:"reason"`
	}
	var out []src
	for i, line := range strings.Split(user, "\n") {
		_ = i
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var n int
		var u string
		if _, err := fmt.Sscanf(line, "[%d] %s", &n, &u); err != nil {
			continue
		}
		out = append(out, src{N: n, Stance: stance[u], Quote: quote[u], Reason: "테스트 이유"})
	}
	b, _ := json.Marshal(map[string]any{"sources": out, "summary": "요약 한 줄"})
	return "앞말 " + string(b) + " 뒷말", nil
}

func TestSourceCheckSupportedWithVerbatimQuotes(t *testing.T) {
	quoteA := "탑솔라는 2024년 3월 KS 인증을 취득했다."
	quoteB := "KS 인증 취득 기업 명단에 탑솔라가 올라 있다."
	pages := map[string]string{
		"https://a.example/news": longBody(quoteA),
		"https://b.example/list": longBody(quoteB),
	}
	stubSourceCheck(t, pages, func(user string) (string, error) {
		return judgeByURL(user,
			map[string]string{"https://a.example/news": "supports", "https://b.example/list": "supports"},
			map[string]string{"https://a.example/news": quoteA, "https://b.example/list": quoteB})
	})
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "탑솔라는 2024년 KS 인증을 받았다"})
	if res.Verdict != VerdictSupported {
		t.Fatalf("verdict = %s (%+v)", res.Verdict, res)
	}
	if res.Confidence != 0.8 {
		t.Errorf("confidence = %v, want 0.8 for two agreeing sources", res.Confidence)
	}
	if res.Fetched != 2 || len(res.Sources) != 2 {
		t.Fatalf("fetched %d sources %d", res.Fetched, len(res.Sources))
	}
	for _, s := range res.Sources {
		if !s.Verbatim || s.Offset < 0 {
			t.Errorf("%s: quote should be verbatim with an offset, got verbatim=%v offset=%d", s.URL, s.Verbatim, s.Offset)
		}
		if !strings.HasPrefix(s.body[s.Offset:], s.Quote) {
			t.Errorf("%s: offset does not point at the quote", s.URL)
		}
		if len(s.SHA256) != 64 {
			t.Errorf("%s: sha256 = %q", s.URL, s.SHA256)
		}
	}
	if !strings.Contains(res.Summary, "요약 한 줄") {
		t.Errorf("judge summary should be appended: %q", res.Summary)
	}
	text := renderSourceCheck(res)
	for _, want := range []string{"판정: **supported**", "인용(원문 그대로", "```json", "\"verdict\": \"supported\""} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered text lacks %q:\n%s", want, text)
		}
	}
}

func TestSourceCheckConflictingStancesIsUnclear(t *testing.T) {
	pages := map[string]string{
		"https://a.example/yes": longBody("인증을 받았다."),
		"https://b.example/no":  longBody("인증을 받은 적이 없다."),
	}
	stubSourceCheck(t, pages, func(user string) (string, error) {
		return judgeByURL(user,
			map[string]string{"https://a.example/yes": "supports", "https://b.example/no": "contradicts"},
			map[string]string{"https://a.example/yes": "인증을 받았다.", "https://b.example/no": "인증을 받은 적이 없다."})
	})
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "인증을 받았다"})
	if res.Verdict != VerdictUnclear || res.Confidence != 0.3 {
		t.Fatalf("verdict = %s conf %v, want unclear 0.3", res.Verdict, res.Confidence)
	}
}

func TestSourceCheckParaphrasedQuoteIsNotVerbatim(t *testing.T) {
	pages := map[string]string{"https://a.example/p": longBody("납기는 12주로 합의됐다.")}
	stubSourceCheck(t, pages, func(user string) (string, error) {
		return judgeByURL(user, map[string]string{"https://a.example/p": "supports"},
			map[string]string{"https://a.example/p": "납기 12주 합의"}) // paraphrase, not in the body
	})
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "납기는 12주다"})
	if len(res.Sources) != 1 || res.Sources[0].Verbatim || res.Sources[0].Offset != -1 {
		t.Fatalf("paraphrase must be flagged: %+v", res.Sources)
	}
	if !strings.Contains(renderSourceCheck(res), "의역 가능성") {
		t.Error("rendered text should warn about the paraphrase")
	}
}

func TestSourceCheckNoUsablePagesIsMissingEvidence(t *testing.T) {
	stubSourceCheck(t, map[string]string{}, func(string) (string, error) { t.Fatal("judge must not run without sources"); return "", nil })
	// A search hit whose fetch fails (unknown page) is skipped, not counted.
	sourceSearchFn = func(context.Context, string, int) (string, []searchResult, string, string, error) {
		return "", []searchResult{{Title: "dead", URL: "https://dead.example/x", Description: "x"}}, "", "", nil
	}
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "아무 주장"})
	if res.Verdict != VerdictMissingEvidence || res.Confidence != 0 || res.Fetched != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestSourceCheckJudgeFailureKeepsTheEvidence(t *testing.T) {
	pages := map[string]string{"https://a.example/p": longBody("판정 모델이 죽어도 발췌는 남는다.")}
	stubSourceCheck(t, pages, func(string) (string, error) { return "", errors.New("model down") })
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "발췌는 남는다"})
	if res.Verdict != VerdictMissingEvidence || len(res.Sources) != 1 || res.Sources[0].Stance != "unclear" {
		t.Fatalf("got %+v", res)
	}
	if len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "판정 모델 실패") {
		t.Errorf("failure must be reported in notes: %v", res.Notes)
	}
}

func TestSourceCheckEmptyPrimaryJudgeFallsBackToTinyRole(t *testing.T) {
	// Live run 2026-09-06: the lightweight cloud twin spent its budget reasoning and
	// returned truncated JSON. The no-think tiny role then answers the same prompt.
	quote := "인증 취득 사실이 공고됐다."
	pages := map[string]string{"https://a.example/p": longBody(quote)}
	stubSourceCheck(t, pages, func(string) (string, error) { return "", nil }) // primary: blank
	sourceJudgeFallbackFn = func(_ context.Context, _ string, user string, _ int) (string, error) {
		return judgeByURL(user, map[string]string{"https://a.example/p": "supports"}, map[string]string{"https://a.example/p": quote})
	}
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "인증을 취득했다"})
	if res.Verdict != VerdictSupported || !res.Sources[0].Verbatim {
		t.Fatalf("fallback verdict not applied: %+v", res)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "tiny 롤 폴백") {
		t.Errorf("fallback must be noted: %v", res.Notes)
	}
}

func TestSourceCheckDomainsFilterKeepsOnlyOfficialHosts(t *testing.T) {
	pages := map[string]string{
		"https://kats.go.kr/cert":  longBody("공식 인증 명단."),
		"https://blog.example/opn": longBody("블로그 의견."),
	}
	var judged []string
	stubSourceCheck(t, pages, func(user string) (string, error) {
		for _, line := range strings.Split(user, "\n") {
			if strings.HasPrefix(line, "[") {
				judged = append(judged, line)
			}
		}
		return judgeByURL(user, map[string]string{"https://kats.go.kr/cert": "supports"}, map[string]string{"https://kats.go.kr/cert": "공식 인증 명단."})
	})
	res := runSourceCheck(context.Background(), NewFetchCache(), nil, nil, SourceCheckInput{Claim: "인증 명단", Domains: []string{"go.kr"}})
	if len(res.Sources) != 1 || res.Sources[0].URL != "https://kats.go.kr/cert" {
		t.Fatalf("domain filter leaked: %+v", res.Sources)
	}
	if len(judged) != 1 {
		t.Errorf("judge saw %d sources, want 1", len(judged))
	}
}

func TestAggregateVerdictRule(t *testing.T) {
	mk := func(stances ...string) []SourceCheckSource {
		out := make([]SourceCheckSource, len(stances))
		for i, s := range stances {
			out[i].Stance = s
		}
		return out
	}
	cases := []struct {
		name string
		in   []SourceCheckSource
		want SourceVerdict
		conf float64
	}{
		{"one support", mk("supports"), VerdictSupported, 0.65},
		{"three supports cap", mk("supports", "supports", "supports"), VerdictSupported, 0.95},
		{"only contradict", mk("contradicts", "unclear"), VerdictContradicted, 0.65},
		{"split", mk("supports", "contradicts"), VerdictUnclear, 0.3},
		{"all unclear", mk("unclear", "unclear"), VerdictMissingEvidence, 0},
		{"nothing", nil, VerdictMissingEvidence, 0},
	}
	for _, tc := range cases {
		v, c := aggregateVerdict(tc.in)
		if v != tc.want || c != tc.conf {
			t.Errorf("%s: got %s %.2f, want %s %.2f", tc.name, v, c, tc.want, tc.conf)
		}
	}
}

func TestAllowedDomainSuffixMatch(t *testing.T) {
	if !allowedDomain("https://kats.go.kr/x", []string{"go.kr"}) || allowedDomain("https://notgo.kr/x", []string{"go.kr"}) {
		t.Error("suffix match must be on a label boundary")
	}
	if !allowedDomain("https://anything.example/", nil) {
		t.Error("empty list allows everything")
	}
}

func TestSourceCheckToolRejectsEmptyClaim(t *testing.T) {
	fn := ToolSourceCheck(NewFetchCache(), nil, nil)
	if _, err := fn(context.Background(), json.RawMessage(`{"claim":"  "}`)); err == nil {
		t.Fatal("empty claim must be rejected")
	}
}
