package webtools

import (
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/browseops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
)

// Register registers web retrieval and browser fallback tools.
func Register(registry toolport.ToolRegistrar, spill tooldeps.SpilloverStore) {
	webCache := web.NewFetchCache()
	localAI := web.NewLocalAIExtractor()
	// Same resident sidecar wiki recall and code search use. Nil when it is not
	// configured, which leaves provider order untouched.
	web.SetSearchReranker(airerank.NewFromEnv())

	registry.RegisterTool(toolport.ToolDef{
		Name: "web",
		Description: "Web access — search and/or fetch pages in one tool. " +
			"query: keyword search (Serper→Brave→DuckDuckGo). " +
			"queries: up to 5 parallel searches. " +
			"url: fetch a page (HTML extract + bot evasion). " +
			"**YouTube 링크는 web이 아니라 watch 툴을 쓰세요** (web은 유튜브를 처리하지 않고 watch로 안내한다). " +
			"fetch=1..3 with query: search then auto-fetch top N pages. " +
			"type=news|scholar|autocomplete: Serper-only typed search (incompatible with fetch).",
		InputSchema: schema.WebToolSchema(),
		Fn:          web.MergedTool(webCache, localAI, spill),
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "browse",
		Description: "상주 실브라우저(서버의 headful Chromium, 운영자가 noVNC로 로그인해 둔 세션 보유)로 페이지를 열어 본문 텍스트를 읽는다. " +
			"web 도구가 막히는 곳에 쓴다: 로그인 필요 페이지(그룹웨어 웹·포털·카페·멤버십), JS 렌더가 무거운 SPA, 봇 감지에 걸리는 사이트. " +
			"공개 정적 페이지는 web이 더 빠르니 web 먼저. 읽기 전용(클릭·입력 없음)·http(s)만. " +
			"로그인이 풀려 있으면 운영자에게 noVNC 재로그인(scripts/browser/start-browser-sidecar.sh view)을 안내하라.",
		InputSchema: schema.BrowseToolSchema(),
		Fn:          browseops.ToolBrowse(),
		Deferred:    true,
	})
	// Claim verification on top of the same search/fetch/excerpt parts: one call
	// returns a fixed-rule verdict plus per-source stance and verbatim quotes.
	registry.RegisterTool(toolport.ToolDef{
		Name: "source_check",
		Description: "주장 하나를 웹 출처로 검증해 판정 아티팩트로 돌려준다: verdict(supported·contradicted·unclear·missing-evidence) + 출처별 입장(supports/contradicts/unclear)과 원문 인용(오프셋·sha256). " +
			"검색→랭크→최대 5페이지 읽기→발췌→판정을 한 번에 한다. '이게 사실이야?', '근거 있어?', 메일·위키의 수치·인증·날짜 주장 확인에 쓴다. " +
			"판정 규칙은 고정 집계다 — 지지만 있으면 supported, 반박만 있으면 contradicted, 둘 다면 unclear, 아무 근거도 없으면 missing-evidence. " +
			"인용의 verbatim=false는 의역이니 그대로 옮기지 말 것. 단순 검색·페이지 읽기는 web을 쓴다.",
		InputSchema: schema.SourceCheckToolSchema(),
		Fn:          web.ToolSourceCheck(webCache, localAI, spill),
		Deferred:    true,
	})
}
