---
description: 모델 역할(main/tiny/lightweight/coding/fallback/vision)별 작업 배치의 단일 진실원 — 어떤 임무가 어떤 역할을 쓰고 왜. 새 LLM 호출 추가·역할 변경 시 필독.
globs: gateway-go/internal/ai/modelrole/**, gateway-go/internal/pipeline/pilot/**, gateway-go/internal/runtime/server/server_chat_config.go
---

# Model Roles → Tasks (역할별 작업 배치)

> 어떤 임무가 어떤 모델 역할을 쓰는지의 **단일 진실원**. 실제 모델 이름은 코드에 하드코딩하지 않는다 — 코드는 **역할만 고르고**, 역할→모델은 `~/.deneb/deneb.json` 의 `agents.*Model` + wormhole 라우터가 결정한다. 새 LLM 호출을 추가하거나 역할을 바꿀 때 아래 "임무→역할 표"에 행을 추가하고 근거를 적는다.

## 역할 6종 + 의도

상수: `gateway-go/internal/ai/modelrole/registry.go`. 모델 매핑: `~/.deneb/deneb.json` `agents.*Model` (예시는 *현재값*일 뿐 — 코드 판단 기준이 아니다).

> ★ **현재값 칼럼은 스냅샷(2026-07-07 갱신)이다.** 오퍼레이터가 네이티브 모델 픽커로 수시로 바꾸므로 문서를 믿지 말고 **판단 전에 실측**하라: 프로덕션 호스트에서 `~/.deneb/deneb.json` 의 `agents.*Model` 키를 직접 읽는다. 이 문서가 과거에 "main=로컬 dsv4"를 서술하는 동안 실제 main 은 2026-06-28부터 클라우드 glm-5.2 였다 — 그 드리프트가 이 경고의 근거다.
>
> 2026-07-17 가용성 사실: dsv4 는 **srv2 단일노드 `vllm-dsv4`**(sparkfleet launch 가 복구 경로), wormhole 의 dsv4 별칭 2종에 **엔트리별 fallback→qwen3.6**(srv1 로컬 `:8000`)이 걸려 있어 dsv4 다운에도 lightweight/tiny 는 생존한다. **adaptive effort router 는 전 wormhole 모델 개통**(deneb.json `providers.wormhole.routing.enabled` + `DENEB_ADAPTIVE_EFFORT=1`) — 단순 대화 턴은 glm 도 thinking-off 로 ~5s. kimi 프로바이더는 402(멤버십 만료, 운영자 갱신 대기).
>
> 2026-08-02 **dsv4 thinking 기본 ON (모델 레이어)**: 0731 서빙이 템플릿 기본을 non-thinking 으로 뒤집어, 세션 레벨 미설정 시 dsv4 챗 런 — 특히 클라우드→dsv4 **폴백 턴** — 이 조용히 무추론으로 돌았다. `fillDualModeDefaultThinking`(chat/run_capability.go, 폴백 체인에도 배선)이 듀얼모드(토글 보유) 모델에 enabled-adaptive 를 기본 주입한다. 세션 "off" 는 존중, effort router 의 단순턴 off 도 그대로, tiny 역할 강제 off 도 무영향(raw 경로). 판정 주의 2가지: ① 응답 추론은 vLLM `reasoning` 필드로 온다(`reasoning_content` 아님 — llm 클라이언트는 양쪽 파싱) ② 쉬운 프롬프트는 적응형 모델이 추론 0자로 즉시 닫는 게 **정상** — 쉬운 프로브로 "thinking 고장" 을 판정하지 말 것 (2026-08-01~02 이틀간의 오판정 사례).

| 역할 | 상수 | 의도 | 로컬/클라우드 (2026-07-17 스냅샷) |
|---|---|---|---|
| main | `RoleMain` | **대화형** 턴 — 사용자 대면 최고 지능 (가장 강력) | ⚠️ **클라우드** (kimi/kimi-for-coding 구독제, ≥07-17) |
| main2 | `RoleMain2` | **opt-in 제2 메인** — main급 품질의 **난이도-라우팅 수신자**: 명백히 단순한 대화 턴(볼륨 대부분)이 여기로 흘러 플래그십 쿼터를 분석급 턴에 보존. **main과 상호 폴백 페어**: main 죽으면 main2가, main2 죽으면 main이 1순위로 이어받아(같은 티어 품질 보존) 그다음에야 lightweight로 강등. 미설정 시 체인 자동 스킵+라우팅 off(단일 main 동작) | 클라우드 (wormhole/glm-5.2 구독제) |
| submain | `RoleSubmain` | **opt-in 자율 배경 레인** — heartbeat·phone-event 인입을 대화형 main 구독에서 분리해 실어 main 처리 여력을 확보하고, (세션 격리와 함께) 자동 턴이 live `client:main` 컨텍스트를 압축·오염하지 않게 한다. main2가 제외하는 automation 트래픽을 의도적으로 수신. 미설정 시 부재(호출자 `""`→main). | 클라우드 (wormhole/glm-5.2 구독제, 정액이라 비용중립) |
| coding | `RoleCoding` | 코드 수정·구현자 서브에이전트·스킬 패치 | 클라우드 (glm-5.2) |
| lightweight | `RoleLightweight` | **바운드 요약**·로컬 잡일꾼 | 로컬 (wormhole/dsv4-nothink@srv2 — qwen3.6에서 교체) |
| tiny | `RoleTiny` | **단순 분류/추출** (가장 작음) | 로컬 (wormhole/dsv4-nothink) |
| fallback | `RoleFallback` | 폴백 체인 **최종 안전망** (상호 main 페어 도입 후 별도 클라우드 종량제 지정은 은퇴 — 로컬로 충분) | 로컬 (wormhole/deepseek-v4-flash@srv2) |
| vision | `RoleVision` | 이미지 턴 (#2510) | 클라우드 (google/gemini-3.5-flash) |

> ★ **main/main2 분리는 모델 분리지 페르소나 분리가 아니다** (2026-07-17, 운영자 설계): 단일 비서 페르소나 원칙은 그대로 — 두 구독제(kimi·glm)에 트래픽을 나누고 서로가 서로의 1순위 폴백이 되는 가용성 페어. 체인: main→main2→lightweight→fallback / main2→main→lightweight→fallback.
>
> ★ **분배 축은 난이도다 — "대화형 vs 배경"이 아니다** (운영자 정정, 2026-07-17): 배경 임무도 고난이도가 많고(딥리서치·병합판정), 대화 턴의 대부분은 단순하다. 구현은 `difficulty_route.go`(chat) — effort router의 캘리브레이션된 단순성 휴리스틱(router.Decide: 짧은 대화체 + 무거운 최근 문맥 없음)으로 **명백-단순 대화 턴만 main2로 스왑**. 자동화(cron/heartbeat/acp)·서브에이전트·명시 모델 지정·첨부 턴은 제외. 조립 후·클라이언트 사용 전에 스왑하므로 하류(캐시 정책·튜닝·폴백)는 네이티브 main2 턴과 동일하고, 오판(false-simple)의 실패는 main2→main 상호 체인으로 플래그십에 낙하 — main 티어 밑으로는 절대 안 떨어진다. 게이트: `DENEB_ADAPTIVE_EFFORT` on + `agents.main2Model` 설정. **2:1 비율 거버너**(운영자 지시): main-티어 턴 카운터(인메모리)로 main2 점유율을 1/(1+2)=1/3로 캡 — 단순 턴이라도 main2가 제 몫을 채웠으면 main1 유지("ratio" 로그), 전 턴이 단순하면 정확히 3턴마다 1턴이 main2(결정적, 무작위성 없음). 분석/자동화 턴이 많은 날은 main2가 1/3 **미만**으로 내려간다(초과는 불가). **배경 main 임무(모닝레터·위키 리서치/리뷰·메일 stage2)는 main 유지** — glm 시절 품질이 실증됐어도 난이도 축에선 main급이 맞고, 재배치는 이 표의 행 단위로만.

> ⚠️ **analysis 역할은 2026-07-07 제거됐다.** 과거 `analysis → glm-5.2`(클라우드, main·chatbot·coding과 공유)라 요약류 헬퍼 콜을 얹으면 클라우드로 새던 함정의 근원이었다 — 컴팩션·youtube 요약이 실제로 이렇게 샜다가 #2508/#2509로 lightweight(로컬)로 환원됐다. 이제 **내부/배경 요약은 lightweight(로컬), 사용자가 읽는 품질 종합은 main**으로 이분된다. analysis와 동일 모델(glm-5.2)이라 제거는 동작상 무변화였다.
>
> ⚙️ **tiny 역할은 thinking 무조건 off** (2026-07-22, 운영자 지시 — 코드로 강제): tiny 는 품질보다 **속도·동시성** 우선(단순 분류/추출·세션 제목·라이브 '생각 중' 칩 요약)이라 chain-of-thought 는 순수 오버헤드다. thinking-off 를 **모델이 아니라 역할 속성**으로 강제한다 — `modelrole.Registry.ThinkingOffDirectiveForRole` 이 per-model 정책이 thinking 을 켜둔(추론모델) 경우에도 vLLM-backed 프로바이더면 `enable_thinking=false` 를 얹는다(pilot 직접경로 `CallRoleLLM`). 계기: tiny 모델이 `dsv4-nothink`(무-think)→`qwen3.6-35b-a3b`(듀얼모드 추론)로 스왑되며 thinking 이 켜진 채 돌아 요약이 ~2배 느리고(1.5s 타임아웃 근접→부하 시 드롭) 가끔 중복/장황했다(라이브 실측: `enable_thinking=false` 0.44s 깔끔 vs 무토글 ~1s). 역할 강제라 이후 모델 스왑에도 무회귀. 같은 실패 모드 전례=워크피드 카드 각주(thinking-off 미적용→256토큰을 추론이 소진→빈응답).
>
> ⚠️ **2026-07 현재는 main도 클라우드다.** 폴백 방향이 뒤집혔다: 클라우드 main(glm)이 죽으면 **로컬 dsv4로 낙하**한다(가용성 관점에선 건강한 배치). 실측(2026-07-04) glm 소비 ≈ 3.2M input tok/일, main 턴 평균 54~73s. vLLM APC(prefix cache) 핫패스는 main이 아니라 **dsv4 경로**(fallback·dsv4-nothink 헬퍼)로 축소됐다 — `docs/agent-rules/prompt-cache.md` §1.5 의 원칙은 그 트래픽과 main 의 로컬 복귀 대비로 그대로 준수한다.
>
> ★ **chatbot 역할은 2026-07-08 제거됐다** — 챗봇 워크스페이스(`chat:` 세션) 자체가 제품에서 삭제되면서(단일 업무 워크스페이스로 통합) `RoleChatbot`·`agents.chatbotModel`도 함께 은퇴했다. 레거시 `chat:` 세션은 일반 세션으로 흡수되어 main을 쓴다.

## 역할 선택 헬퍼 (`pipeline/pilot/localai.go`)

| 호출 | 역할 |
|---|---|
| **에이전트 턴** (cron/chat 합성) | main (`agents.defaultModel`) |
| ~~`pilot.CallAnalysisLLM`~~ | **retire됨 (2026-07-07)** — analysis 역할 제거로 삭제. 품질 종합은 main 턴 또는 `CallRoleLLM(RoleMain, …)`으로 |
| ~~`pilot.CallCodingLLM`~~ | **retire됨 (2026-07-05)** — 호출자 0으로 삭제. 코딩 역할 소비는 run_model.go·genesis 경로가 직접 수행하며, 원샷 헬퍼가 다시 필요하면 `CallRoleLLM(RoleCoding, …)` 한 줄로 재생성 |
| `pilot.CallLocalLLM` | lightweight |
| `pilot.CallTinyLLM` | tiny |
| `(*Server).mailAnalysisModels()` | stage2 = main, stage1 = tiny |

## 임무 → 역할 표

> 근거 칼럼의 "로컬/클라우드" 언급은 각 행 작성 시점의 스냅샷이다. 임무→**역할** 배치가 이 표의 불변 내용이고, 역할→모델(로컬 여부)은 위 스냅샷 표와 deneb.json 실측이 기준.

| 임무 | 위치 | 역할 | 근거 |
|---|---|---|---|
| 일간/모닝레터 합성 | `tools/routine/morning_letter.go` 결정적 수집 → `cronrunner` 1회 무도구 의미 투영 → `morning_card.go` 고정 렌더 | **main** | 사용자가 읽는 프로젝트별 중요도·맥락·후행 제안은 품질 종합이라 main. 모델은 JSON 의미 슬롯만 1턴 채우고 양식·수치·이스케이프는 서버가 소유한다. 모델/채팅 장애 시 동일 수집값의 사실 전용 카드로 fail-open. 수동 요청은 도구의 결정적 `delivery`를 그대로 반환 |
| 프로젝트 위키 딥리서치 갱신 (6h) | `runtime/wikiwork/wiki_research_task.go` | **main** | 도구무거운 에이전트 턴(내부 소스 재조사→위키 본문 갱신·supersede). boot/heartbeat/goal과 동형의 에이전트 턴이라 main, 헬퍼 요약 콜 아님. wiki-research 프리셋(웹 제외)으로 내부 소스 한정, 로컬 |
| 메일 리포트 종합 (stage2) | `mailAnalysisModels()` | **main** | 사용자가 읽는 리포트, 품질 최우선 — analysis 제거로 main(가장 강력·의도적 클라우드 OK) |
| 메일 추출 (stage1) · gmail facts/actions/deal | `mailAnalysisModels()`, `platform/mailanalysis/pipeline_extractors.go` | **tiny** | 단순 구조화 JSON 추출 |
| 거래 조건 인용 추출 (deal facts 2차 패스) | `platform/mailanalysis/deal_facts.go` | **tiny** | 거래 메일 한정 — 물량·단가·지급조건·하자보수·지체상금을 **원문 인용 필수**로 추출, Go 결정적 게이트(`verifyDealFacts`: 인용⊂원문 + 값 숫자⊂인용)가 미검증 필드 드롭. stage1과 같은 배치·같은 예산, fail-open |
| 세션 자동 제목 | `chat/session_autotitle.go` | **tiny** | 짧은 명사구 제목 |
| 워크피드 카드 제목+요약 | `runtime/proactive/workfeed_title_llm.go` | **tiny** | 짧은 제목 + 2줄 카드 요약을 단일 호출로 생성 — 세션 자동제목과 같은 단순 추출이라 tiny. lightweight였을 땐 클라우드 추론모델(deepseek-v4-flash-api)로 폴백 시 thinking-off 미적용→256토큰을 추론이 소진→빈응답→휴리스틱 폴백(라이브 확인: tiny 1.0–1.4초 정상). 휴리스틱(extractCardTitle/Summary)이 여전히 최종 폴백 |
| goal 루프 judge | `runtime/goalloop/goal_task.go` | **lightweight** | 바운드 판정(DONE/CONTINUE), fail-open |
| 위키 리뷰어 중복 판정 | `runtime/wikiwork/wiki_review_task.go` (2h 자율 태스크) | **main** (⚠️클라우드) | 최근 쓰인 위키 문서의 근사중복 판정 — 결정적 후보 수집(FindSimilarPages)→단일 JSON 판정 콜→결정적 병합(FoldDuplicate, 상한·git 스냅샷·기본 관찰모드 `DENEB_WIKI_REVIEW_AUTOMERGE`). **왜 lightweight가 아닌가**: 오판정 high가 실제 페이지 두 장을 병합하는 파괴적 판단이라 판정 품질 우선 — 운영자 명시 지시(2026-07-02). analysis 제거로 품질 의도는 가장 강력한 main이 승계(2026-07-07). 호출량 극소(2h당 ≤1콜, 후보 없으면 0콜)라 비용 바운드. 스킬리뷰 교훈(텍스트 역할 toolCount=0) 반영해 도구호출 없는 bounded 파이프라인, fail-open |
| 컴팩션 청크 요약 | `chat/run_prepare.go` `localAISummarizer` | **lightweight** | 내부 무손실 요약, 로컬·빠름 (#2508; 이전 analysis-클라우드가 #2489 타임아웃 주원인) |
| youtube 자막 요약 | `chat/web/web_youtube.go` | **lightweight** | 충실도 요약, 로컬 (#2509) |
| watch 영상 전사 분석 | `chat/tools/artifact/watch.go` | **lightweight** | 자막 기반 분석 요약 |
| 도구출력 압축 | `chat/localai_hooks.go` | **lightweight** | 큰 입력 압축 |
| polaris 검색/요약 헬퍼 | `runtime/server/chat_pipeline.go` (`LocalAIFunc`) | **lightweight** | 회상 핫패스 |
| 구현자 서브에이전트 | `sessions_spawn(tool_preset=implementer)` | **coding** when configured, else subagent/default | 파일 쓰기·exec를 할 수 있는 코드 수정 위임. 코딩 전용 모델은 네이티브 설정의 `agents.codingModel`로 opt-in |
| 스킬 진화 패치 생성 | `init_genesis.go` → `genesis.Evolver` | **coding** when configured, else lightweight | SKILL.md 실제 수정 후보를 만드는 경로. 코딩 전용 모델 설정 시 main teacher rewrite를 끄고 coding 역할이 패치 생성을 담당 |
| 스킬 리뷰 (nudger fork → propose 결정) | `init_genesis.go` reviewModel → `runtime/skilllifecycle/skill_review_fork.go` SendSync | **coding** (이전 lightweight), coding 미설정 시 lightweight 폴백 | 리뷰는 `skill_lifecycle action=propose`를 **호출**해야 하는 도구호출 task. lightweight는 텍스트 역할(요약·제목·JSON — Deneb 어디서도 도구호출 안 함)이라 산문만 내고 **toolCount=0**(srv4 실측) → 전 리뷰 no-op, Propus 루프 무산출. evolver와 같은 도구-가능 역할로 정렬(dogma #7: 도구 무거운 역할엔 측정된 도구호출자). 리뷰는 backoff·2048토큰 캡 + **전용 미니 시스템 프롬프트**(메인 조립 ~50K 비상속 — 리뷰당 입력 62-68K→~12K, 스킬 인덱스는 skills action=list로 온디맨드)로 빈도/비용 바운드 |
| 스킬 진화 behavioral replay (도구호출 회귀 검증) | `domain/skills/genesis/validation_executor.go` (executor) + `init_genesis.go` 배선 | **lightweight** | 후보 SKILL.md를 부작용 없이 시뮬레이션해 도구호출 plan을 뽑고, original↔candidate 행동 회귀를 채점하는 검증 게이트. 로컬·바운드. **왜 lightweight인가**: main은 챗 핫패스와 GPU 경합 + 과비용이고, 게이트는 두 본문을 **같은 모델**로 비교하므로 절대 충실도보다 일관된 판별력이 중요(executor 편향은 델타에서 상쇄). `DENEB_SKILL_EVOLVE_REPLAY`로 opt-in, fail-open |
| 프로젝트별 최신 근황 digest | `domain/wiki/project_digest.go` (드림 사이클 Phase 3d) → 프로젝트 대표페이지 `## 현재 상태` 섹션 (`project_status.go`) → `miniapp.project.digests` (모아보기 화면) | **lightweight** | 드림 사이클이 소비하는 그 일지/메모 입력을 프로젝트별로 롤업(헤드라인+불릿 2~3)하는 내부 배경 요약. open_loops와 동형의 격리된 1콜, fail-open, 실제 `프로젝트/*` 페이지에 앵커. **왜 lightweight인가**: 컴팩션·youtube와 같은 내부 배경 요약 도그마(로컬·바운드) — analysis(클라우드)로 둘 이유 없음. 화면은 대표페이지 섹션만 읽어 LLM 핫패스 아님. 메일분석 이벤트 갱신(`wiki_mail_analysis.go` → `AppendProjectStatusLine`)은 **LLM 없는 결정적 날짜 불릿**이라 아래 표 참조 |
| 위키 자료 인제스트 요약 (`wiki action="ingest"` URL/유튜브 캡처) | `chat/tools/wikitool/wiki_ingest.go` | **lightweight** | 컴팩션·youtube와 동형의 내부 배경 요약 (도그마 #1). 바운드(입력 16K룬·출력 700tok), fail-open — 요약 실패 시 발췌로 캡처 자체는 보존, force 재인제스트로 재시도 |
| 부팅 점검 (기동 30s 후 + 24h 데일리 자가점검) | `runtime/heartbeat/boot_task.go`, 배선 `server_workflow_capabilities.go` (`fallbackRoleIfConfigured`) | **fallback** (설정 시), 미설정 시 **main** | 기동 직후=클라우드 신뢰성이 가장 낮은 순간의 자가점검이라 **로컬 상주 안전망 모델이 의미상 정확한 집** (2026-08-01 재배치 1호). 판단 저난도("알릴 게 있나")·저빈도(1/일+재시작당 1회)·오판 비용 극소. no-think 0731 품질 실증(7/7·함정분석) 후 첫 행 단위 이동 |
| 폰 이벤트 판단턴 (알림·문맥·클립보드 → 능동 알림) | `runtime/phoneevents/handler.go` (`processJudgment`), 배선 `server/phone_event_config.go` | **submain** (설정 시), 미설정 시 **main** | 자율 배경 레인 격리 — 고빈도(하루 ~47회, 전체 입력 토큰 ~19%) 판단턴을 대화형 main(kimi) 구독에서 glm 서브메인으로 이전해 main 처리 여력을 확보. glm 정액이라 비용중립. 도구호출 역할이나 별도 벤치 불요 — 이미 coding 역할로 실증된 glm 재사용(도그마 #7). 폰 이벤트는 이미 `phone-event:*` 격리 세션이라 **모델만 전환**, tiny-gate(`worthFullJudgment`)는 그대로 tiny 유지. 미설정 시 부재로 무동작 |
| 하트비트 30분 점검턴 | `runtime/heartbeat/heartbeat_task.go` (`Run`), 배선 `server/server_workflow_capabilities.go` | **submain** (설정 시), 미설정 시 **main** | 최대 자율 배경 소비자(하루 ~17회, 전체 입력 토큰 ~35%)를 서브메인으로 이전. **핵심은 모델보다 세션 격리** — 기존엔 `client:main`에서 돌며 매 틱 사용자 대화를 강제 압축(12K 예산)해 라이브 세션이 세부를 잃던 주범. 이제 `submain:heartbeat` 독립 세션에서 추론하고, 리포트는 `RelayNative`(→client:main+푸시)로 별도 배달(`AutoDeliveredOutput`로 in-loop message 도구 비활성). 미설정 시 세션 격리·배달은 그대로 적용되고 모델만 main |

## LLM 안 쓰는 곳 (의도적)

| 임무 | 위치 | 방식 | 왜 |
|---|---|---|---|
| 웹페이지 인앱 번역 (en/ru→ko) | `chat/tools/translate.go` → `translate_deepl.go` | DeepL API (외부 번역 서비스) | 인앱 브라우저 인플레이스 번역은 **DeepL 전용**. LLM 역할 미사용 — DeepL 미설정/실패 시 그 배치는 원문 유지(개수 보존, 드롭/재정렬 금지). 이전 `translation` 역할 LLM 폴백은 폐기(2026-07-07) |
| 주간업무보고 | `tools/routine/weekly_report.go` | 결정적 양식 | byte-identical 출력 (#2474) |
| 메일 우선순위 분류 | `domain/mailpriority/score.go` | 정규식 점수 + 결합 신호 | 글랜스 트리아지 — 한국 업무메일 튜닝 휴리스틱. VIP(주소록)·활성 거래처(`wiki.ActiveCounterpartyDomains` — 최근 60일 프로젝트 연결 메일분석의 발신 도메인, freemail 제외, 서버 10분 캐시) 부스트는 콘텐츠 신호가 있을 때만 증폭(단독 발화 금지 — 글랜스성 보존). 같은 견적·금액 메일도 진행 거래처면 urgent로 — 단일 이벤트가 아닌 결합 신호(시나리오) 원칙 |
| 카드 제목 폴백 | `runtime/proactive/workfeed_extract.go` | 휴리스틱 추출 | LLM 실패 시 graceful degradation |
| 하트비트 리서치 넛지 (새 데이터 다이제스트) | `runtime/heartbeat/heartbeat_research.go` | 위키 mtime 스캔 + 마커 스로틀 (~1회/일) | 신규 유입 카운트는 결정적으로, 교차 패턴 판단·"[연구]" 항목 등록은 **하트비트 턴(main)** 이 수행 — 스캔 자체에 LLM 0회 |
| 하트비트 자가코딩 검토 넛지 | `runtime/heartbeat/heartbeat_selfcoding.go` | 제안됨 카운트+지문 마커 (변경 즉시·불변 24h 재시도) | 대기 후보 감지는 결정적으로, 검토·실행·리뷰 기록은 **하트비트 턴(main)** 이 skill_lifecycle로 수행 — 포집만 되고 소비 안 되던 자가코딩 큐(7/5 실측: 첫 정상 가동 2건이 1일+ 방치)를 자동 소비 |
| 스킬 자동 힌트 (관련 스킬 표면화) | `chat/skill_hints.go` → `run_tail_inject.go` | 결정적 트리거 매칭 (frontmatter `triggers` ⊂ 사용자 메시지, 턴당 ≤2) | 매 턴 핫패스 — 인메모리 스냅샷 매칭이면 충분. 힌트는 마지막 user 메시지 wire-only 꼬리(APC-safe) |
| 프로젝트 현재 상태 이벤트 갱신 (메일분석) | `runtime/server/wiki_mail_analysis.go` → `domain/wiki/project_status.go:AppendProjectStatusLine` | 결정적 날짜 불릿 | 메일분석 시 관련 프로젝트 대표페이지 `## 현재 상태`에 한 줄 append (idempotent by mail id). 8h 드림 사이클을 안 기다리고 즉시 최신화 — 주기적 LLM 압축은 드리머가 |
| 메일분석 당사자 앵커 | `platform/mailanalysis/party_anchor.go` (stage2 프롬프트 주입) | 헤더 파싱 + 우리측 도메인 셋 (`DENEB_MAIL_OUR_DOMAINS`) | 발신/수신/참조의 소속(우리 측/외부)을 결정적으로 명시해 분석 모델의 당사자 뒤집기 제거 — 실메일 섀도런 검증(2026-07-05, 모델 무관 이득). 형식은 scripts/dev/mail-bench.py 앵커와 동기 유지 |
| 메일분석 날짜 앵커 | `platform/mailanalysis/date_anchor.go` (stage2 프롬프트 주입) | Date 헤더 파싱 + 상대 날짜 환산표(발송 주·다음 주·그 다음 주, 월요일 시작 KST) | "다음 주 금요일" 류를 계산이 아닌 표 조회로 — 상대 날짜 산술은 측정된 모델 약점(dsv4 두 형식 모두 오답, 2026-07-04 벤치). 실측: 앵커로 3/3 절대날짜+요일 명기, 무앵커 시 1/3이 상대 표현만 반복(절대 날짜 미기재). 헤더 미파싱 시 무주입 fail-open |
| 폰 앱 사용 리듬 캐시 (`usage_update`) | `runtime/phoneevents/handler.go:recordPhoneUsage` → `chat/tools/runtimeops/phone_usage.go` (`phone_read what=usage`) | 캐시 전용 (파일 mtime=신선도, location_update와 동형) | 이전 "usage" 이벤트는 6시간마다 judgment 턴을 태우는데 guidance가 기본 침묵이라 거의 항상 NO_REPLY — 하루 ~4턴 낭비. 사용 리듬은 능동 알림 소스가 아니라 조회용 맥락이므로 캐시로 환원, 에이전트가 필요할 때 phone_read로 읽는다. 구 클라이언트의 "usage" 타입은 기존 judgment 경로 유지 (OTA 전환기 하위호환) |

## 도그마

1. **내부/배경 요약 → lightweight(로컬).** 컴팩션·youtube·compress·watch·polaris. 클라우드 main으로 두지 마라.
2. **사용자가 읽는 품질 종합 → main.** 메일 리포트 종합, 일간레터.
3. **단순 분류/추출 → tiny.** 제목, JSON 필드 추출.
4. **결정적 포맷·트리아지 → LLM 없음.** 주간보고, 우선순위.
5. **★ analysis 역할은 2026-07-07 제거됐다.** 내부 요약은 lightweight(로컬), 사용자 품질은 main. 요약류 헬퍼 콜을 클라우드 main에 얹으면 샌다 — "왜 로컬 lightweight로 안 되나"를 답 못 하면 lightweight를 써라. (닥스트링이 `CallLocalLLM`/local을 가리키는데 코드가 `CallRoleLLM(RoleMain)`이면 그건 드리프트 — 원복하라.)
6. **코드에 모델 이름 하드코딩 금지.** 역할만 고른다.
7. **★ 도구 무거운 역할(main/fallback)에 새 모델을 배선하기 전, 후보의 도구호출 역량을 측정하라.** 챗 `main`은 ~50개 내장 도구(스키마 `tool_schemas.json`)를 쓰고 도구호출이 에이전트의 성패를 가른다 — `/v1/models` 200·속도만으로는 빈 `tool_calls`(서빙설정 미스로 도구가 안 나오는 인프라 오진단의 단골)나 프롬프트 인젝션 취약을 못 잡는다. SparkFleet의 `run_tool_eval`(tool-eval-bench 래퍼)로 그 엔드포인트를 벤치해 **멀티스텝 체인·에러복구·Category K(안전·프롬프트 인젝션)** 점수를 확인하고 배선한다(결과 회독: `tool_eval_history`). 이건 코드 게이트가 아니라 **운영자 승격 절차**다 — 게이트웨이는 모델을 소비만 하고, 검증은 플릿 매니저(sparkfleet)에서 한다.
8. **★ 텍스트 역할(lightweight/tiny) 교체 후보는 `scripts/dev/lightweight-model-ab.py`로 실부하 A/B 후 승격하라.** 공개 벤치(특히 에이전트 벤치)는 이 역할의 실제 임무 — 한국어 압축 요약(프로덕션 4-섹션 스켈레톤)·JSON 추출·짧은 제목·바운드 판정(한 단어 + goal judge JSON 계약)·알림 트리아지(YES/NO) — 를 측정하지 않는다. 이 스크립트가 그 5종을 결정적 채점(사실 보존 체크리스트·JSON 파싱·형식 규칙 + 레이턴시/장황함)으로 비교한다: wormhole에 두 모델을 서빙해 두고 `python3 scripts/dev/lightweight-model-ab.py --model-a <현역> --model-b <후보>` → **교체하려는 역할의 서브버딕트**(`AB_VERDICT_TINY`=extract·title·triage / `AB_VERDICT_LIGHTWEIGHT`=compaction·verdict) 우세 + 레이턴시/토큰 비열화 확인 후 deneb.json 역할 매핑 교체. `json_mode=rejected`(JSON 모드 400 거부)가 뜬 후보는 총점과 무관하게 승격 불가 — 프로덕션 gmail stage1은 formatless 폴백이 없다. (에이전트 튜닝 모델의 전형적 실패 모드 — 요약에 계획 서두, 코드펜스 JSON, 판정에 사족 — 을 채점이 감지함은 `--mock` 셀프테스트로 고정.)

## PR 체크리스트 (새 LLM 호출 / 역할 변경 시)

- [ ] 위 "임무→역할 표"에 행 추가
- [ ] 역할 선택 근거 1줄 (왜 이 역할인가)
- [ ] main(클라우드) 품질 종합 선택 시 "왜 로컬 lightweight로 안 되나" 명시
- [ ] 요약/추출/분류류는 **로컬 lightweight/tiny부터** 검토
- [ ] 도구 무거운 역할(main/fallback) 배선·교체 시: SparkFleet `run_tool_eval`로 후보 모델의 도구호출 역량(특히 Category K·멀티스텝 체인) 확인
- [ ] lightweight/tiny 모델 교체 시: `scripts/dev/lightweight-model-ab.py` A/B 결과(AB_METRIC + 해당 역할의 `AB_VERDICT_TINY`/`AB_VERDICT_LIGHTWEIGHT` 서브버딕트 + `json_mode` + 레이턴시·토큰) 확인
