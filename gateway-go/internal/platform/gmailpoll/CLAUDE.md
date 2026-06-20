# gmailpoll 서브트리 지도 (구조)

> 자율 메일 분석 파이프라인의 **구조적 지도** — 신규 메일을 감지해 추출→합성→소비하는 단계가 어디에 있는지. 모델 역할 배치 정책은 `.claude/rules/model-roles.md`(stage1=tiny, stage2=analysis)가 소관, 여기 복붙하지 않는다. 사이드카(OCR/ASR) 운영은 `.claude/rules/sidecar-models.md`.

## 무엇 / 왜

주기적으로 Gmail을 폴링해 신규 메일을 LLM으로 분석하고, 결과를 네이티브 클라로 보고한다. Deneb "업무분석" 모드의 핵심 능동 데이터 경로. 카카오/IMAP IDLE 트리거로도 단건 분석이 들어온다([project_kakao_mail_pipeline]).

## 디렉토리 맵 (파일)

| 파일 | 역할 |
|---|---|
| `service.go` | 폴링 서비스 — 주기 루프, 신규 감지, 보고 배선 |
| `state.go` | 폴 상태 persist(처리한 메일 추적). 패키지 닥(`Package gmailpoll …`) |
| `pipeline.go` | ★`AnalyzeEmailPipeline` — 분석 오케스트레이터. `PipelineDeps`, 프로젝트 후보 매칭, 로컬 LLM JSON 헬퍼(`callLocalLLMJSON`) |
| `pipeline_synthesis.go` | `AnalyzeEmailPipeline`의 단계들: **stage-1 컨텍스트 추출**(스레드·발신자 기억·위키 그래프) + **stage-2 합성** 호출 + 중요도 판정/관련 프로젝트 suffix 파싱 |
| `pipeline_extractors.go` | 합성된 분석 텍스트 위에서 도는 로컬-AI 추출기: 위키 fact 제안·운영자 action item·거래 정보. 전부 lightweight 모델 JSON 모드 |
| `pipeline_batch.go` | 배치 분석 경로 |
| `analyzer.go` | 분석 보조(프롬프트/파싱) |
| `attachments.go` | 첨부 해석. 본문 추출(OCR/문서파싱)은 pipeline 레이어 경유 |
| `files_archive.go` | 첨부/파일 아카이빙 |
| `mail_body_prep.go` | 본문 정제 |
| `money_normalize.go` | 금액 표기 정규화(한국 업무메일) |
| `reasoning_leak.go` | 추론 누출 스트립([project_cron_narration_leak]) |

## 핵심 흐름: 추출 → 합성 → 소비

```
service.go (주기 폴 / 외부 트리거)
  → AnalyzeEmailPipeline (pipeline.go)
      stage-1: 컨텍스트 추출 (pipeline_synthesis.go)   # tiny 역할 — 스레드·발신자·위키그래프
      stage-2: 합성 (pipeline_synthesis.go)            # analysis 역할 — 사용자가 읽는 리포트
      → 추출기 (pipeline_extractors.go)                # lightweight — 위키facts/actions/deals JSON
  → 보고: 네이티브 클라 (workfeed 카드 / proactive)
  → 소비: 위키 fact 반영 · 캘린더/할일 제안 · 거래 KB
```

- 입력 메일의 첨부는 `mailarchive`(바이트만 반환)에서 가져와 pipeline 레이어가 텍스트 추출. **mailarchive는 추출기를 import하지 않는다**(레이어 경계).

## 흔한 작업 진입점

| 하려는 것 | 시작 파일 |
|---|---|
| 분석 단계 추가/수정 | `pipeline_synthesis.go`(stage1/2). 오케스트레이션은 `pipeline.go:AnalyzeEmailPipeline` |
| 새 추출 종류(facts/actions/deals 류) | `pipeline_extractors.go` — lightweight JSON 추출 패턴 따라 |
| 폴링 주기/감지 로직 | `service.go` + `state.go` |
| 첨부 처리 | `attachments.go` → pipeline OCR 경유 |
| 금액/한국 업무 표기 | `money_normalize.go` |

## 함정

- **모델 역할 직교**: stage1=tiny(단순 구조화 추출), stage2=analysis(사용자가 읽는 합성 — **의도적 클라우드 OK**), 추출기=lightweight. 추출기를 analysis로 올리면 비용·레이턴시가 샌다 — `.claude/rules/model-roles.md` 도그마 5.
- **`mailAnalysisModels()`는 server에 있다**(`runtime/server/`), 역할 해석의 단일 지점. gmailpoll은 그 모델을 소비만.
- **추론 누출 방어**: 분석 텍스트에 모델 self-talk/reasoning이 새는 이력 — `reasoning_leak.go`로 스트립([project_cron_narration_leak]). 본문에 메타발화 의심되면 여기부터.
- **cron 트리거 회귀 이력**: bind·잡이름404·배포폭풍 in-flight abort 3회. cron 복원은 `deliverableLen>0`까지 라이브 검증([project_kakao_mail_pipeline]).
- **dev가 prod cron 공유 실행** → 라이브 검증 시 prod 부수효과(위키 쓰기 등). 검증 후 즉시 stop([reference_livetest_dev_cron_shared]).
- **무응답 실패는 `Error`+broadcast**(`.claude/rules/logging.md`) — 분석 결과가 사용자에게 안 닿으면 평상 로그에 묻히면 안 된다.
