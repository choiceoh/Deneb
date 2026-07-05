---
description: 모델 역할(main/tiny/lightweight/analysis/coding/fallback/chatbot/vision/translation)별 작업 배치의 단일 진실원 — 어떤 임무가 어떤 역할을 쓰고 왜. 새 LLM 호출 추가·역할 변경 시 필독.
globs: gateway-go/internal/ai/modelrole/**, gateway-go/internal/pipeline/pilot/**, gateway-go/internal/runtime/server/server_chat_config.go
---

# Model Roles → Tasks (역할별 작업 배치)

> 어떤 임무가 어떤 모델 역할을 쓰는지의 **단일 진실원**. 실제 모델 이름은 코드에 하드코딩하지 않는다 — 코드는 **역할만 고르고**, 역할→모델은 `~/.deneb/deneb.json` 의 `agents.*Model` + wormhole 라우터가 결정한다. 새 LLM 호출을 추가하거나 역할을 바꿀 때 아래 "임무→역할 표"에 행을 추가하고 근거를 적는다.

## 역할 9종 + 의도

상수: `gateway-go/internal/ai/modelrole/registry.go`. 모델 매핑: `~/.deneb/deneb.json` `agents.*Model` (예시는 *현재값*일 뿐 — 코드 판단 기준이 아니다).

> ★ **현재값 칼럼은 스냅샷(2026-07-04)이다.** 오퍼레이터가 네이티브 모델 픽커로 수시로 바꾸므로 문서를 믿지 말고 **판단 전에 실측**하라: 프로덕션 호스트에서 `~/.deneb/deneb.json` 의 `agents.*Model` 키를 직접 읽는다. 이 문서가 과거에 "main=로컬 dsv4"를 서술하는 동안 실제 main 은 2026-06-28부터 클라우드 glm-5.2 였다 — 그 드리프트가 이 경고의 근거다.

| 역할 | 상수 | 의도 | 로컬/클라우드 (2026-07-04 스냅샷) |
|---|---|---|---|
| main | `RoleMain` | 대화·분석·도구호출·생성물 합성 (가장 강력) | ⚠️ **클라우드** (glm-5.2, ≥06-28) |
| chatbot | `RoleChatbot` | 챗봇 워크스페이스(`chat:`) 전용 | 클라우드 (glm-5.2) |
| analysis | `RoleAnalysis` | **추론급 품질** 종합 (리포트 등) | ⚠️ **클라우드** (glm-5.2) |
| coding | `RoleCoding` | 코드 수정·구현자 서브에이전트·스킬 패치 | 클라우드 (glm-5.2) |
| lightweight | `RoleLightweight` | **바운드 요약**·로컬 잡일꾼 | 로컬 (wormhole/dsv4-nothink@srv2 — qwen3.6에서 교체) |
| tiny | `RoleTiny` | **단순 분류/추출** (가장 작음) | 로컬 (wormhole/dsv4-nothink) |
| fallback | `RoleFallback` | 폴백 체인 | 로컬 (wormhole/deepseek-v4-flash@srv2) |
| vision | `RoleVision` | 이미지 턴 (#2510) | 클라우드 (google/gemini-3.5-flash) |
| translation | `RoleTranslation` | 인앱 브라우저 웹페이지 번역 (en/ru→ko) | 로컬 (wormhole/dsv4-nothink) |

> ⚠️ **analysis 역할의 함정 (여전히 유효).** enum 주석은 "highest-quality **LOCAL** model"이라 적혀 있지만, 현재 deneb.json은 `analysis → glm-5.2`(클라우드)로 지정한다(main·chatbot·coding과 공유). 즉 **analysis 역할에 임무를 얹으면 클라우드로 샌다** — 비용(구독 크레딧) + 레이턴시(~20s/콜). 요약류 헬퍼 콜을 무심코 analysis로 두면 안 된다. 실제로 **컴팩션·youtube 요약이 이렇게 샜다가** #2508/#2509로 lightweight(로컬)로 환원됐다.
>
> ⚠️ **2026-07 현재는 main도 클라우드다.** 폴백 방향이 뒤집혔다: 클라우드 main(glm)이 죽으면 **로컬 dsv4로 낙하**한다(가용성 관점에선 건강한 배치). 실측(2026-07-04) glm 소비 ≈ 3.2M input tok/일, main 턴 평균 54~73s. vLLM APC(prefix cache) 핫패스는 main이 아니라 **dsv4 경로**(fallback·dsv4-nothink 헬퍼·챗봇 세션 오버라이드)로 축소됐다 — `.claude/rules/prompt-cache.md` §1.5 의 원칙은 그 트래픽과 main 의 로컬 복귀 대비로 그대로 준수한다.

## 역할 선택 헬퍼 (`pipeline/pilot/localai.go`)

| 호출 | 역할 |
|---|---|
| **에이전트 턴** (cron/chat 합성) | main (`agents.defaultModel`) |
| `pilot.CallAnalysisLLM` | analysis |
| ~~`pilot.CallCodingLLM`~~ | **retire됨 (2026-07-05)** — 호출자 0으로 삭제. 코딩 역할 소비는 run_model.go·genesis 경로가 직접 수행하며, 원샷 헬퍼가 다시 필요하면 `CallRoleLLM(RoleCoding, …)` 한 줄로 재생성 |
| `pilot.CallLocalLLM` | lightweight |
| `pilot.CallTinyLLM` | tiny |
| `(*Server).mailAnalysisModels()` | stage2 = analysis, stage1 = tiny |

## 임무 → 역할 표

> 근거 칼럼의 "로컬/클라우드" 언급은 각 행 작성 시점의 스냅샷이다. 임무→**역할** 배치가 이 표의 불변 내용이고, 역할→모델(로컬 여부)은 위 스냅샷 표와 deneb.json 실측이 기준.

| 임무 | 위치 | 역할 | 근거 |
|---|---|---|---|
| 일간/모닝레터 합성 | `tools/morning_letter.go`(데이터 수집만) + 크론 에이전트 턴 | **main** | 사용자 일일 브리핑 — 품질·로컬. 도구는 JSON만 반환, 합성은 main 턴 |
| 프로젝트 위키 딥리서치 갱신 (6h) | `runtime/server/wiki_research_task.go` | **main** | 도구무거운 에이전트 턴(내부 소스 재조사→위키 본문 갱신·supersede). boot/heartbeat/goal과 동형의 에이전트 턴이라 main, 헬퍼 요약 콜 아님. wiki-research 프리셋(웹 제외)으로 내부 소스 한정, 로컬 |
| 메일 리포트 종합 (stage2) | `mailAnalysisModels()` | **analysis** | 사용자가 읽는 리포트, 품질 최우선 — **의도적 클라우드 OK** |
| 메일 추출 (stage1) · gmail facts/actions/deal | `mailAnalysisModels()`, `platform/mailanalysis/pipeline_extractors.go` | **tiny** | 단순 구조화 JSON 추출 |
| 거래 조건 인용 추출 (deal facts 2차 패스) | `platform/mailanalysis/deal_facts.go` | **tiny** | 거래 메일 한정 — 물량·단가·지급조건·하자보수·지체상금을 **원문 인용 필수**로 추출, Go 결정적 게이트(`verifyDealFacts`: 인용⊂원문 + 값 숫자⊂인용)가 미검증 필드 드롭. stage1과 같은 배치·같은 예산, fail-open |
| 세션 자동 제목 | `chat/session_autotitle.go` | **tiny** | 짧은 명사구 제목 |
| 코드모드 체크포인트 라벨 | `runtime/server/method_registry.go` `checkpointSummarizer` → `code.AfterTurn` | **tiny** | 코딩 턴의 요청+결과 보고를 한국어 명사구 한 줄(≤40자)로 — 체크포인트 목록이 바이브코더의 변경 이력이라 사용자 메시지 원문보다 "무엇이 바뀌었나"가 맞다. dirty 확인 후에만 호출(읽기 턴 무비용), fail-open(실패 시 사용자 메시지 트림 폴백) |
| 워크피드 카드 제목+요약 | `runtime/server/workfeed_title_llm.go` | **lightweight** | 짧은 제목 + 2줄 카드 요약을 단일 호출로 생성 (#2504 후 lightweight). 휴리스틱(extractCardTitle/Summary)이 폴백 |
| goal 루프 judge | `runtime/server/goal_task.go` | **lightweight** | 바운드 판정(DONE/CONTINUE), fail-open |
| 위키 리뷰어 중복 판정 | `runtime/server/wiki_review_task.go` (2h 자율 태스크) | **analysis** (⚠️클라우드) | 최근 쓰인 위키 문서의 근사중복 판정 — 결정적 후보 수집(FindSimilarPages)→단일 JSON 판정 콜→결정적 병합(FoldDuplicate, 상한·git 스냅샷·기본 관찰모드 `DENEB_WIKI_REVIEW_AUTOMERGE`). **왜 lightweight가 아닌가**: 오판정 high가 실제 페이지 두 장을 병합하는 파괴적 판단이라 판정 품질 우선 — 운영자 명시 지시(2026-07-02). 호출량 극소(2h당 ≤1콜, 후보 없으면 0콜)라 비용 바운드. 스킬리뷰 교훈(텍스트 역할 toolCount=0) 반영해 도구호출 없는 bounded 파이프라인, fail-open |
| 컴팩션 청크 요약 | `chat/run_prepare.go` `localAISummarizer` | **lightweight** | 내부 무손실 요약, 로컬·빠름 (#2508; 이전 analysis-클라우드가 #2489 타임아웃 주원인) |
| youtube 자막 요약 | `chat/web/web_youtube.go` | **lightweight** | 충실도 요약, 로컬 (#2509) |
| watch 영상 전사 분석 | `chat/tools/watch.go` | **lightweight** | 자막 기반 분석 요약 |
| 도구출력 압축 | `chat/localai_hooks.go` | **lightweight** | 큰 입력 압축 |
| polaris 검색/요약 헬퍼 | `runtime/server/chat_pipeline.go` (`LocalAIFunc`) | **lightweight** | 회상 핫패스 |
| 구현자 서브에이전트 | `sessions_spawn(tool_preset=implementer)` | **coding** when configured, else subagent/default | 파일 쓰기·exec를 할 수 있는 코드 수정 위임. 코딩 전용 모델은 네이티브 설정의 `agents.codingModel`로 opt-in |
| 코드모드 세션 턴 (`code:<taskID>`) | `chat/run_model.go` (chatbot 분기와 대칭) | **coding** when configured, else main | 코드모드 채팅 턴. 세션 키로 판별, `agents.codingModel` 설정 시 그 모델·미설정 시 main 폴백(옵트인·무변화). 프롬프트는 구현자 프로파일(`prompt.CodingPersona` + worktree CLAUDE.md/AGENTS.md 주입·업무 컨텍스트 차단), 도구는 `PresetCoding`(업무 메모리·개인 데이터 도구 제외) |
| 스킬 진화 패치 생성 | `init_genesis.go` → `genesis.Evolver` | **coding** when configured, else lightweight | SKILL.md 실제 수정 후보를 만드는 경로. 코딩 전용 모델 설정 시 main teacher rewrite를 끄고 coding 역할이 패치 생성을 담당 |
| 스킬 리뷰 (nudger fork → propose 결정) | `init_genesis.go` reviewModel → `skill_review_fork.go` SendSync | **coding** (이전 lightweight), coding 미설정 시 lightweight 폴백 | 리뷰는 `skill_lifecycle action=propose`를 **호출**해야 하는 도구호출 task. lightweight는 텍스트 역할(요약·제목·JSON — Deneb 어디서도 도구호출 안 함)이라 산문만 내고 **toolCount=0**(srv4 실측) → 전 리뷰 no-op, Propus 루프 무산출. evolver와 같은 도구-가능 역할로 정렬(dogma #7: 도구 무거운 역할엔 측정된 도구호출자). 리뷰는 backoff·2048토큰 캡 + **전용 미니 시스템 프롬프트**(메인 조립 ~50K 비상속 — 리뷰당 입력 62-68K→~12K, 스킬 인덱스는 skills action=list로 온디맨드)로 빈도/비용 바운드 |
| 스킬 진화 behavioral replay (도구호출 회귀 검증) | `domain/skills/genesis/validation_executor.go` (executor) + `init_genesis.go` 배선 | **lightweight** | 후보 SKILL.md를 부작용 없이 시뮬레이션해 도구호출 plan을 뽑고, original↔candidate 행동 회귀를 채점하는 검증 게이트. 로컬·바운드. **왜 lightweight인가**: main은 챗 핫패스와 GPU 경합 + 과비용이고, 게이트는 두 본문을 **같은 모델**로 비교하므로 절대 충실도보다 일관된 판별력이 중요(executor 편향은 델타에서 상쇄). `DENEB_SKILL_EVOLVE_REPLAY`로 opt-in, fail-open |
| 프로젝트별 최신 근황 digest | `domain/wiki/project_digest.go` (드림 사이클 Phase 3d) → 프로젝트 대표페이지 `## 현재 상태` 섹션 (`project_status.go`) → `miniapp.project.digests` (모아보기 화면) | **lightweight** | 드림 사이클이 소비하는 그 일지/메모 입력을 프로젝트별로 롤업(헤드라인+불릿 2~3)하는 내부 배경 요약. open_loops와 동형의 격리된 1콜, fail-open, 실제 `프로젝트/*` 페이지에 앵커. **왜 lightweight인가**: 컴팩션·youtube와 같은 내부 배경 요약 도그마(로컬·바운드) — analysis(클라우드)로 둘 이유 없음. 화면은 대표페이지 섹션만 읽어 LLM 핫패스 아님. 메일분석 이벤트 갱신(`wiki_mail_analysis.go` → `AppendProjectStatusLine`)은 **LLM 없는 결정적 날짜 불릿**이라 아래 표 참조 |

| 웹페이지 인앱 번역 (인플레이스, en/ru→ko) | `chat/tools/translate.go` → `miniapp.web.translate` 브리지 | **translation** (신설, 미설정 시 lightweight 폴백) | 인앱 브라우저가 보낸 DOM 텍스트 세그먼트 배치를 번역. 바운드 로컬 변환이라 기본 lightweight, `agents.translationModel` 로 번역 특화 모델 opt-in. **개수 보존**(LLM 응답이 입력 개수와 불일치하면 그 배치는 원문 유지 — 텍스트 드롭/재정렬 금지) |

## LLM 안 쓰는 곳 (의도적)

| 임무 | 위치 | 방식 | 왜 |
|---|---|---|---|
| 주간업무보고 | `tools/weekly_report.go` | 결정적 양식 | byte-identical 출력 (#2474) |
| 메일 우선순위 분류 | `domain/mailpriority/score.go` | 정규식 점수 + 결합 신호 | 글랜스 트리아지 — 한국 업무메일 튜닝 휴리스틱. VIP(주소록)·활성 거래처(`wiki.ActiveCounterpartyDomains` — 최근 60일 프로젝트 연결 메일분석의 발신 도메인, freemail 제외, 서버 10분 캐시) 부스트는 콘텐츠 신호가 있을 때만 증폭(단독 발화 금지 — 글랜스성 보존). 같은 견적·금액 메일도 진행 거래처면 urgent로 — 단일 이벤트가 아닌 결합 신호(시나리오) 원칙 |
| 카드 제목 폴백 | `runtime/server/workfeed_extract.go` | 휴리스틱 추출 | LLM 실패 시 graceful degradation |
| 하트비트 리서치 넛지 (새 데이터 다이제스트) | `runtime/server/heartbeat_research.go` | 위키 mtime 스캔 + 마커 스로틀 (~1회/일) | 신규 유입 카운트는 결정적으로, 교차 패턴 판단·"[연구]" 항목 등록은 **하트비트 턴(main)** 이 수행 — 스캔 자체에 LLM 0회 |
| 하트비트 자가코딩 검토 넛지 | `runtime/server/heartbeat_selfcoding.go` | 제안됨 카운트+지문 마커 (변경 즉시·불변 24h 재시도) | 대기 후보 감지는 결정적으로, 검토·실행·리뷰 기록은 **하트비트 턴(main)** 이 skill_lifecycle로 수행 — 포집만 되고 소비 안 되던 자가코딩 큐(7/5 실측: 첫 정상 가동 2건이 1일+ 방치)를 자동 소비 |
| 스킬 자동 힌트 (관련 스킬 표면화) | `chat/skill_hints.go` → `run_tail_inject.go` | 결정적 트리거 매칭 (frontmatter `triggers` ⊂ 사용자 메시지, 턴당 ≤2) | 매 턴 핫패스 — 인메모리 스냅샷 매칭이면 충분. 힌트는 마지막 user 메시지 wire-only 꼬리(APC-safe) |
| 프로젝트 현재 상태 이벤트 갱신 (메일분석) | `runtime/server/wiki_mail_analysis.go` → `domain/wiki/project_status.go:AppendProjectStatusLine` | 결정적 날짜 불릿 | 메일분석 시 관련 프로젝트 대표페이지 `## 현재 상태`에 한 줄 append (idempotent by mail id). 8h 드림 사이클을 안 기다리고 즉시 최신화 — 주기적 LLM 압축은 드리머가 |
| 메일분석 당사자 앵커 | `platform/mailanalysis/party_anchor.go` (stage2 프롬프트 주입) | 헤더 파싱 + 우리측 도메인 셋 (`DENEB_MAIL_OUR_DOMAINS`) | 발신/수신/참조의 소속(우리 측/외부)을 결정적으로 명시해 분석 모델의 당사자 뒤집기 제거 — 실메일 섀도런 검증(2026-07-05, 모델 무관 이득). 형식은 scripts/dev/mail-bench.py 앵커와 동기 유지 |
| 메일분석 날짜 앵커 | `platform/mailanalysis/date_anchor.go` (stage2 프롬프트 주입) | Date 헤더 파싱 + 상대 날짜 환산표(발송 주·다음 주·그 다음 주, 월요일 시작 KST) | "다음 주 금요일" 류를 계산이 아닌 표 조회로 — 상대 날짜 산술은 측정된 모델 약점(dsv4 두 형식 모두 오답, 2026-07-04 벤치). 실측: 앵커로 3/3 절대날짜+요일 명기, 무앵커 시 1/3이 상대 표현만 반복(절대 날짜 미기재). 헤더 미파싱 시 무주입 fail-open |
| 폰 앱 사용 리듬 캐시 (`usage_update`) | `runtime/server/server_http_event_ingest.go:recordPhoneUsage` → `chat/tools/phone_usage.go` (`phone_read what=usage`) | 캐시 전용 (파일 mtime=신선도, location_update와 동형) | 이전 "usage" 이벤트는 6시간마다 judgment 턴을 태우는데 guidance가 기본 침묵이라 거의 항상 NO_REPLY — 하루 ~4턴 낭비. 사용 리듬은 능동 알림 소스가 아니라 조회용 맥락이므로 캐시로 환원, 에이전트가 필요할 때 phone_read로 읽는다. 구 클라이언트의 "usage" 타입은 기존 judgment 경로 유지 (OTA 전환기 하위호환) |

## 도그마

1. **내부/배경 요약 → lightweight(로컬).** 컴팩션·youtube·compress·watch·polaris. 클라우드 analysis로 두지 마라.
2. **사용자가 읽는 품질 종합 → analysis(또는 main).** 메일 리포트 종합, 일간레터.
3. **단순 분류/추출 → tiny.** 제목, JSON 필드 추출.
4. **결정적 포맷·트리아지 → LLM 없음.** 주간보고, 우선순위.
5. **★ analysis 역할은 현재 클라우드다.** 헬퍼 콜을 여기 얹으면 샌다. "왜 로컬 lightweight로 안 되나"를 답 못 하면 lightweight를 써라. (닥스트링이 `CallLocalLLM`/local을 가리키는데 코드가 `CallAnalysisLLM`이면 그건 드리프트 — 원복하라.)
6. **코드에 모델 이름 하드코딩 금지.** 역할만 고른다.
7. **★ 도구 무거운 역할(main/fallback)에 새 모델을 배선하기 전, 후보의 도구호출 역량을 측정하라.** 챗 `main`은 150+ 도구를 쓰고 도구호출이 에이전트의 성패를 가른다 — `/v1/models` 200·속도만으로는 빈 `tool_calls`(서빙설정 미스로 도구가 안 나오는 인프라 오진단의 단골)나 프롬프트 인젝션 취약을 못 잡는다. SparkFleet의 `run_tool_eval`(tool-eval-bench 래퍼)로 그 엔드포인트를 벤치해 **멀티스텝 체인·에러복구·Category K(안전·프롬프트 인젝션)** 점수를 확인하고 배선한다(결과 회독: `tool_eval_history`). 이건 코드 게이트가 아니라 **운영자 승격 절차**다 — 게이트웨이는 모델을 소비만 하고, 검증은 플릿 매니저(sparkfleet)에서 한다.
8. **★ 텍스트 역할(lightweight/tiny) 교체 후보는 `scripts/dev/lightweight-model-ab.py`로 실부하 A/B 후 승격하라.** 공개 벤치(특히 에이전트 벤치)는 이 역할의 실제 임무 — 한국어 압축 요약(프로덕션 4-섹션 스켈레톤)·JSON 추출·짧은 제목·바운드 판정(한 단어 + goal judge JSON 계약)·알림 트리아지(YES/NO) — 를 측정하지 않는다. 이 스크립트가 그 5종을 결정적 채점(사실 보존 체크리스트·JSON 파싱·형식 규칙 + 레이턴시/장황함)으로 비교한다: wormhole에 두 모델을 서빙해 두고 `python3 scripts/dev/lightweight-model-ab.py --model-a <현역> --model-b <후보>` → **교체하려는 역할의 서브버딕트**(`AB_VERDICT_TINY`=extract·title·triage / `AB_VERDICT_LIGHTWEIGHT`=compaction·verdict) 우세 + 레이턴시/토큰 비열화 확인 후 deneb.json 역할 매핑 교체. `json_mode=rejected`(JSON 모드 400 거부)가 뜬 후보는 총점과 무관하게 승격 불가 — 프로덕션 gmail stage1은 formatless 폴백이 없다. (에이전트 튜닝 모델의 전형적 실패 모드 — 요약에 계획 서두, 코드펜스 JSON, 판정에 사족 — 을 채점이 감지함은 `--mock` 셀프테스트로 고정.)

## PR 체크리스트 (새 LLM 호출 / 역할 변경 시)

- [ ] 위 "임무→역할 표"에 행 추가
- [ ] 역할 선택 근거 1줄 (왜 이 역할인가)
- [ ] analysis(클라우드) 선택 시 "왜 로컬 lightweight로 안 되나" 명시
- [ ] 요약/추출/분류류는 **로컬 lightweight/tiny부터** 검토
- [ ] 도구 무거운 역할(main/fallback) 배선·교체 시: SparkFleet `run_tool_eval`로 후보 모델의 도구호출 역량(특히 Category K·멀티스텝 체인) 확인
- [ ] lightweight/tiny 모델 교체 시: `scripts/dev/lightweight-model-ab.py` A/B 결과(AB_METRIC + 해당 역할의 `AB_VERDICT_TINY`/`AB_VERDICT_LIGHTWEIGHT` 서브버딕트 + `json_mode` + 레이턴시·토큰) 확인
