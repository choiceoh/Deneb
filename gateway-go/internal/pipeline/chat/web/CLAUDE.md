# Web 수집 pipeline 변경 지도

이 패키지는 agent의 web search/fetch 입력을 안전한 HTTP 수집, 형식별 추출,
bounded 결과 envelope로 변환한다. media·document parser를 조합하지만 문서/OCR
구현이나 LLM turn orchestration 자체는 소유하지 않는다.

## 진입점과 소유권

- `web_fetch.go`의 `Tool`이 url, query, queries, search+fetch 모드를 분기하고
  cache와 singleflight를 거쳐 결과를 조립한다. 메타에 `FetchMs`/`Provider`를
  넣고 slog로 fetch/extract 지연을 남긴다. `webFetchURLDetailed`는 envelope와
  함께 `assessMetaBody` 가용 판정을 반환한다. search+fetch 후보는
  `web_fetch_rank.go`의 `rankFetchCandidates`(answerBox·knowledgeGraph·스니펫
  겹침·다양성·denylist)로 고른 뒤 `fillUsableFetches`가 상위 2개를 병렬 wave로
  받고 부족분만 순차 보충하며, deny/fill 통계를 slog로 남긴다.
- `web_fetch_search.go`는 Serper→Brave→DuckDuckGo 순으로 검색한다. 키 부재뿐
  아니라 프로바이더 실패도 다음으로 폴백하며 `web search fallback` slog를 남긴다.
  Hangul 쿼리는 Serper `gl=kr`/`hl=ko`, Brave `country=KR`/`search_lang=ko`를
  붙인다. Serper knowledgeGraph website도 fetch 후보에 포함한다. DDG Instant
  Answer는 fetchable organic URL이 없다.
- `web_http.go`의 `FetchRaw`, `SharedClient`가 pooled SSRF-safe transport와
  구조화된 fetch error 경계를 노출한다.
- `web_content.go`가 content type 분류와 metadata/error envelope를,
  `web_html.go`의 `LocalAIExtractor`, `NewLocalAIExtractor`가 HTML 정제를
  담당한다. 기본 추출은 htmlmd이고 LocalAI는 대용량·저retention에만 게이트된다.
  binary document는 `tools/document`로 위임한다.
- `web_fetch_stealth.go`는 stage 간 고정 sleep 없이 Chrome→(soft-block 시)
  Firefox→**상주 브라우저 사이드카(:18930, `web_fetch_browser.go`)**→Jina로
  올리고, SPA shell(`js_required`/`empty_body`)은 Firefox를 건너뛰고 렌더
  스테이지로 간다. Jina는 사이드카 실패 시에만 도는 외부 최후 폴백.
  `web_fetch_tier.go`의 도메인별 티어 메모리(state dir `~/.deneb/web-stealth-tiers.json`,
  7d TTL·24h 하향 프로브)가 시작 스테이지를 고른다. 사이드카 디스패치는
  `media.ValidatePublicTarget`으로 사설망을 차단한다(티어 점프가 SSRF-safe
  transport를 우회할 수 있어 필수). Serper scrape 타임아웃은 10s fail-fast.
- thin-content 에스컬레이션(`web_fetch_escalate.go`)도 같은 순서: 로컬
  사이드카 렌더 먼저, Jina는 그 실패 시에만.
- `archive.go`는 fetch된 document만 `filestore`에 보존하며 일반 HTML은
  archive하지 않는다.

## 의존 방향과 불변조건

- 의존 방향은 `toolwire/chat → web → media/document/liteparse/pilot`다. web은
  tool registry나 chat runner를 import하지 않고, media의 SSRF dialer를 우회하는
  별도 transport를 만들지 않는다.
- `Tool`은 query batch를 5개, auto-fetch를 3개로 제한하고 전체 `maxChars` 예산을
  분배한다. cache에는 full extraction을 저장하고 요청별 truncation은 반환 시에만
  적용해야 한다.
- 동일 URL의 동시 fetch는 singleflight로 하나만 실행하며 leader panic이 다음
  요청의 key를 오염시키거나 type assertion panic으로 전파되면 안 된다.
- SPA thin-content escalation은 `js_required`/`empty_body` 신호가 함께 있을 때
  한 번만 실행하고, 더 풍부한 결과일 때만 원본을 교체한다.
- YouTube의 전체 transcript는 spillover에 보존하고 main conversation에는 bounded
  summary/fallback만 반환한다.

## 테스트와 집중 검증

- `web_boundary_contract_test.go`의
  `TestContentClassificationReturnsTypeAndDocumentName`,
  `TestSharedClientUsesPooledTransportAndIndependentTimeouts`,
  `TestRetryabilityAndClassificationErrorMatrix`가 fetch 경계를 검증한다.
- `singleflight_test.go`의 `TestSingleflightRecoversKeyAfterPanic`,
  `web_fetch_escalate_test.go`의 `TestEscalateThinContentUpdatesMetaWithRicherResult`,
  `web_youtube_test.go`의 `TestFormatYouTubeFallback_TruncatesAndReferencesSpillover`
  가 주요 degrade 경로를 고정한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/web`
