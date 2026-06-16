---
description: 모델 역할(main/tiny/lightweight/analysis/fallback/chatbot/vision)별 작업 배치의 단일 진실원 — 어떤 임무가 어떤 역할을 쓰고 왜. 새 LLM 호출 추가·역할 변경 시 필독.
globs: gateway-go/internal/ai/modelrole/**, gateway-go/internal/pipeline/pilot/**, gateway-go/internal/runtime/server/server_chat_config.go
---

# Model Roles → Tasks (역할별 작업 배치)

> 어떤 임무가 어떤 모델 역할을 쓰는지의 **단일 진실원**. 실제 모델 이름은 코드에 하드코딩하지 않는다 — 코드는 **역할만 고르고**, 역할→모델은 `~/.deneb/deneb.json` 의 `agents.*Model` + wormhole 라우터가 결정한다. 새 LLM 호출을 추가하거나 역할을 바꿀 때 아래 "임무→역할 표"에 행을 추가하고 근거를 적는다.

## 역할 7종 + 의도

상수: `gateway-go/internal/ai/modelrole/registry.go`. 모델 매핑: `~/.deneb/deneb.json` `agents.*Model` (예시는 *현재값*일 뿐 — 코드 판단 기준이 아니다).

| 역할 | 상수 | 의도 | 로컬/클라우드 (현재 예시) |
|---|---|---|---|
| main | `RoleMain` | 대화·분석·도구호출·생성물 합성 (가장 강력) | 로컬 (deepseek-v4-flash) |
| chatbot | `RoleChatbot` | 챗봇 워크스페이스(`chat:`) 전용 | (config) |
| analysis | `RoleAnalysis` | **추론급 품질** 종합 (리포트 등) | ⚠️ **현재 클라우드** (glm-5.2) |
| lightweight | `RoleLightweight` | **바운드 요약**·로컬 잡일꾼 | 로컬 (qwen3.6-35b) |
| tiny | `RoleTiny` | **단순 분류/추출** (가장 작음) | 로컬 (qwen3.6-35b) |
| fallback | `RoleFallback` | 폴백 체인 | (config) |
| vision | `RoleVision` | 이미지 턴 (#2510) | (config) |

> ⚠️ **analysis 역할의 함정.** enum 주석은 "highest-quality **LOCAL** model"이라 적혀 있지만, 현재 deneb.json은 `analysis → glm-5.2`(클라우드)로 지정한다(chatbot·fallback과 공유). 즉 **analysis 역할에 임무를 얹으면 클라우드로 샌다** — 비용(구독 크레딧) + 레이턴시(~20s/콜). 요약류 헬퍼 콜을 무심코 analysis로 두면 안 된다. 실제로 **컴팩션·youtube 요약이 이렇게 샜다가** #2508/#2509로 lightweight(로컬)로 환원됐다.

## 역할 선택 헬퍼 (`pipeline/pilot/localai.go`)

| 호출 | 역할 |
|---|---|
| **에이전트 턴** (cron/chat 합성) | main (`agents.defaultModel`) |
| `pilot.CallAnalysisLLM` | analysis |
| `pilot.CallLocalLLM` | lightweight |
| `pilot.CallTinyLLM` | tiny |
| `(*Server).mailAnalysisModels()` | stage2 = analysis, stage1 = tiny |

## 임무 → 역할 표

| 임무 | 위치 | 역할 | 근거 |
|---|---|---|---|
| 일간/모닝레터 합성 | `tools/morning_letter.go`(데이터 수집만) + 크론 에이전트 턴 | **main** | 사용자 일일 브리핑 — 품질·로컬. 도구는 JSON만 반환, 합성은 main 턴 |
| 메일 리포트 종합 (stage2) | `mailAnalysisModels()` | **analysis** | 사용자가 읽는 리포트, 품질 최우선 — **의도적 클라우드 OK** |
| 메일 추출 (stage1) · gmail facts/actions/deal | `mailAnalysisModels()`, `platform/gmailpoll/pipeline_extractors.go` | **tiny** | 단순 구조화 JSON 추출 |
| 세션 자동 제목 | `chat/session_autotitle.go` | **tiny** | 짧은 명사구 제목 |
| 워크피드 카드 제목+요약 | `runtime/server/workfeed_title_llm.go` | **lightweight** | 짧은 제목 + 2줄 카드 요약을 단일 호출로 생성 (#2504 후 lightweight). 휴리스틱(extractCardTitle/Summary)이 폴백 |
| goal 루프 judge | `runtime/server/goal_task.go` | **lightweight** | 바운드 판정(DONE/CONTINUE), fail-open |
| 컴팩션 청크 요약 | `chat/run_prepare.go` `localAISummarizer` | **lightweight** | 내부 무손실 요약, 로컬·빠름 (#2508; 이전 analysis-클라우드가 #2489 타임아웃 주원인) |
| youtube 자막 요약 | `chat/web/web_youtube.go` | **lightweight** | 충실도 요약, 로컬 (#2509) |
| watch 영상 전사 분석 | `chat/tools/watch.go` | **lightweight** | 자막 기반 분석 요약 |
| 도구출력 압축 | `chat/localai_hooks.go` | **lightweight** | 큰 입력 압축 |
| polaris 검색/요약 헬퍼 | `runtime/server/chat_pipeline.go` (`LocalAIFunc`) | **lightweight** | 회상 핫패스 |

## LLM 안 쓰는 곳 (의도적)

| 임무 | 위치 | 방식 | 왜 |
|---|---|---|---|
| 주간업무보고 | `tools/weekly_report.go` | 결정적 양식 | byte-identical 출력 (#2474) |
| 메일 우선순위 분류 | `domain/mailpriority/score.go` | 정규식 점수 | 글랜스 트리아지 — 한국 업무메일 튜닝 휴리스틱 |
| 카드 제목 폴백 | `runtime/server/workfeed_extract.go` | 휴리스틱 추출 | LLM 실패 시 graceful degradation |

## 도그마

1. **내부/배경 요약 → lightweight(로컬).** 컴팩션·youtube·compress·watch·polaris. 클라우드 analysis로 두지 마라.
2. **사용자가 읽는 품질 종합 → analysis(또는 main).** 메일 리포트 종합, 일간레터.
3. **단순 분류/추출 → tiny.** 제목, JSON 필드 추출.
4. **결정적 포맷·트리아지 → LLM 없음.** 주간보고, 우선순위.
5. **★ analysis 역할은 현재 클라우드다.** 헬퍼 콜을 여기 얹으면 샌다. "왜 로컬 lightweight로 안 되나"를 답 못 하면 lightweight를 써라. (닥스트링이 `CallLocalLLM`/local을 가리키는데 코드가 `CallAnalysisLLM`이면 그건 드리프트 — 원복하라.)
6. **코드에 모델 이름 하드코딩 금지.** 역할만 고른다.
7. **★ 도구 무거운 역할(main/fallback)에 새 모델을 배선하기 전, 후보의 도구호출 역량을 측정하라.** 챗 `main`은 150+ 도구를 쓰고 도구호출이 에이전트의 성패를 가른다 — `/v1/models` 200·속도만으로는 빈 `tool_calls`(서빙설정 미스로 도구가 안 나오는 인프라 오진단의 단골)나 프롬프트 인젝션 취약을 못 잡는다. SparkFleet의 `run_tool_eval`(tool-eval-bench 래퍼)로 그 엔드포인트를 벤치해 **멀티스텝 체인·에러복구·Category K(안전·프롬프트 인젝션)** 점수를 확인하고 배선한다(결과 회독: `tool_eval_history`). 이건 코드 게이트가 아니라 **운영자 승격 절차**다 — 게이트웨이는 모델을 소비만 하고, 검증은 플릿 매니저(sparkfleet)에서 한다.

## PR 체크리스트 (새 LLM 호출 / 역할 변경 시)

- [ ] 위 "임무→역할 표"에 행 추가
- [ ] 역할 선택 근거 1줄 (왜 이 역할인가)
- [ ] analysis(클라우드) 선택 시 "왜 로컬 lightweight로 안 되나" 명시
- [ ] 요약/추출/분류류는 **로컬 lightweight/tiny부터** 검토
- [ ] 도구 무거운 역할(main/fallback) 배선·교체 시: SparkFleet `run_tool_eval`로 후보 모델의 도구호출 역량(특히 Category K·멀티스텝 체인) 확인
