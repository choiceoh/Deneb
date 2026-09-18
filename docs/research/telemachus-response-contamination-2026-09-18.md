# 텔레마코스 응답 오염 — 제품 측 수정 기록 (2026-09-18)

**사건**: Deneb stream_0043, 2026-09-17 20:49 KST. 50,005토큰 요청(T=1, seed 7, thinking, reasoning_budget 24,576, top_p 1)의 한국어 응답이 중간부터 오염(언어 붕괴 → 문서 이어쓰기 → 툴콜 마크업). 레코드 = 트랜스크립트 해시 `ef529aa06c55f3c02b3045f81ffbc149aec2de38526b1b67a925d1ab1330b154`. 원본 프롬프트·출력·로짓은 저장소 밖에 둔다 — 여기에는 해시와 카운터만 남긴다.

**엔진 판정(엔진 저장소, 요청서 §2)**: 엔진 무죄. 같은 요청 2회 bit 동일, 난수 2,313개 호스트 계산과 일치, 스케일·배율·FP32 복원 arm 전부 여전히 오염. 모델의 실제 선호 토큰이 gen0 `Follow`/`User`/`Simple`/`Context`(영어 헤딩), gen3 `' I'` 0.706 vs 추첨된 `' 이'` 0.108 — 여기서 분기 후 gen4+ 자기조건화(median p 0.992). greedy로도 argmax가 `' I'`라 해결 안 됨. **모델이 "답변 모드"가 아니라 "문서 이어쓰기 모드"였다.**

## 1. 요청서 R1 판정 정정 — 절반만 사실

요청서는 "현재 마지막 한국어 지시는 5만 토큰 전이고 바로 앞 맥락은 영어 에이전트 스캐폴딩"이라 했다. 코드 대조 결과:

| 요청서 주장 | 코드 사실 | 근거 |
|---|---|---|
| 바로 앞 맥락 = 영어 스캐폴딩 | **아님.** 마지막 user 메시지 꼬리의 마지막 블록은 한국어 산문 `[전달 정책 — 이번 턴]`, 그 뒤 템플릿의 assistant·thinking 마커 | `gateway-go/internal/pipeline/chat/run_tail_inject.go` `buildTailAdditions` 순서(회상→피드→스킬힌트→보드→전달정책→카드거부). `AutoDeliveredOutput`은 모든 네이티브 인터랙티브 챗에서 true |
| 마지막 "한국어로 답변" 지시 = 5만 토큰 전 | **맞음.** 유일한 언어 지시는 Static 시스템 블록의 `Always respond in Korean` (프롬프트 머리) | `gateway-go/internal/pipeline/chat/prompt/system_prompt.go` communication 섹션 |
| assistant 턴 시작 마커를 추가하라 | **중복.** 템플릿이 이미 붙인다 | 사건 원본 꼬리 확인(운영자) |

즉 꼬리 블록들은 한국어로 *쓰여* 있을 뿐 언어를 *지정*하지 않았고, 머리와 꼬리 사이 5만 토큰은 대부분 영어형 기록(근거 행·트랜스크립트 발췌·툴 JSON)이었다. gen3에서 모델이 `' I'`를 0.706으로 선호한 것은 언어 앵커가 걸리지 않은 상태와 정합한다.

## 2. 변경 (제품 측만 — 엔진 변경 없음)

### R1′ — 응답 언어·모드 앵커 (꼬리 마지막)

`responseLanguageAnchor` (`gateway-go/internal/pipeline/chat/run_tail_inject.go`): 모든 영속 user 턴의 꼬리 **마지막** 추가분. "위의 기록을 이어쓰지 말고, 사용자의 마지막 메시지에 한국어로 답하라." 상수 바이트라 `tail_register`가 런 경계에서 byte-identical 재부착 → APC 접두사 무손상, 비용 ≈ 60토큰. `EphemeralUser`(하트비트·부트 자기트리거·notifier)는 자체 NO_REPLY/`## status` 계약을 유지하므로 제외. prompt-cache §1.5 꼬리 주입 원칙 준수(system 불변).

### R2 — 기록을 데이터로 격리

- **렌더 분리**: `chatport.ChatMessage.ExcerptText()` (`gateway-go/internal/pipeline/chatport/transcript_contracts.go`) — 모델에 보여주는 발췌 전용. `SearchableText()`(검색 *매칭*)에서 (a) 어시스턴트 `thinking` 본문, (b) 툴콜 인자 JSON을 제거. 툴 *이름*(`[도구 x]`)은 유지 — "무엇이 일어났나"는 내용이고, JSON은 모델이 흉내 낸 마크업이다. 검색 매칭·polaris FTS 인덱스는 변경 없음.
- **봉투**: `sessions(action=history|search)` 결과를 `<transcript-excerpt source="sessions" trust="untrusted">` + 한국어 System note로 감싼다 (`gateway-go/internal/pipeline/chat/tools/sessionops/sessions_tool.go`). 추론에서만 일치한 검색 행은 `(내부 추론에서 일치 — 본문 생략)`으로 표기 — 검색이 보여줄 수 없는 것을 찾았다고 주장하지 않는다. 오류·무일치 응답은 기록이 아니므로 봉투 없음.
- **동일 렌더러 적용**: `polaris(action=expand)` (`gateway-go/internal/pipeline/chat/tools/recallops/polaris.go`).
- **신뢰 경계 일반화**: 시스템 프롬프트 `## Historical Context Boundary`가 `<transcript-excerpt … trust="untrusted">`도 명시 (Static 블록 1회 캐시 미스 후 안정).
- 회상 근거(`<recall-context>`)는 이미 봉투·System note·close tag를 갖고 있고 세션 행 노트는 `TextContent()`(추론 미포함)로 추출한다 — 변경 없음.

### R3 — 디코딩 정책: 변경 없음

T=1·top_p=1은 유지. 요청서 자체가 밝힌 대로 분기 구조에서 비선호 분기를 ~30% 확률로 집는 것은 정상 동작이고 greedy로도 해결되지 않는다. 이어쓰기 분기를 막는 것은 R1′/R2다.

### R4 — 엔진 측 가드: 이 레포 범위 밖

## 3. 수용 기준 — A1 정정

요청서 A1("동일 id에 R1만 적용한 재생")은 자기모순이다: 지시를 끼워 넣으면 id가 바뀐다. 대조 실험으로 다시 쓴다.

> **A1′.** 같은 원본(캡처된 프롬프트 텍스트 또는 id 시퀀스)에서 두 arm을 조립한다. arm A = 원본 그대로(= **A3 대조군**), arm B = 마지막 assistant 마커 직전에 앵커 블록만 삽입. **통제 검증**: `sha256(A[:off]) == sha256(B[:off])` 이고 `B == A[:off] + 앵커 + A[off:]`임을 디코드 전에 단언. 그 위에서 T=1·seed 7·max 2048·캐시 미사용으로 두 arm을 디코드한다.
>
> - A1′ 통과 = arm B에서 언어 붕괴(`collapse_at<0`)·툴 마크업(`tool_markup_hits=0`) 소멸, 한글 비율 ≥ 0.5
> - **A2** = arm B 초기 8토큰에 `Follow`/`User`/`Simple`/`Context` 류 미출현
> - **A3** = arm A에서 오염 재현 (수정이 원인을 건드렸음을 증명)

**결정적 절반(레포 안)**: `TestTailAnchorIsAControlledSuffixOfTheWireMessage` (`gateway-go/internal/pipeline/chat/run_tail_anchor_test.go`)가 게이트웨이 조립에서 같은 통제 속성(접두사 해시 일치·엄격한 접미사·바이트 안정)을 단언한다. `TestTelemachusReplayAnchorMatchesGateway`가 스크립트의 앵커 텍스트를 게이트웨이 상수에 고정한다.

**엔진 절반(레포 밖 데이터)**: `scripts/dev/telemachus_ab_replay.py` — 원본 파일을 받아 두 arm을 만들고 통제 검증 후 디코드·채점한다. 출력은 해시·카운터 JSON만; 원문 덤프는 `--dump-dir`이 저장소 밖일 때만.

```bash
python3 scripts/dev/telemachus_ab_replay.py --engine http://<engine> --model glm-5.3-flash --prompt-file /data/incidents/stream_0043.prompt.txt --cache-salt
```

id 모드(`--ids-file`)는 엔진 `/tokenize`가 `tokens` 리스트를 돌려줄 때만 가능하다(현행 Go 클라이언트는 `count`만 읽는다).

## 4. 후속 감사 (2026-09-18, 엔진 무관 — Deneb 조립 경로의 추가 결함)

운영자 요청("ST 엔진을 떠나 데네브 그 부분의 문제를 더 찾아봐")으로 꼬리 주입·회상 렌더·세션/polaris 도구·압축 요약·와이어 변환을 훑었다. 결함 아님으로 판정한 것: 압축 요약기 입력은 thinking을 이미 제외(`gateway-go/internal/pipeline/compaction/llm.go` `serializeMessages`); 과거 thinking을 `reasoning_content`로 되돌리는 와이어 정책(`gateway-go/internal/ai/llm/openai.go` `reasoningHistoryEchoed`)은 접두사 안정성 근거가 문서화돼 있고 GLM 템플릿이 과거 턴 추론을 버림; `tailForSystem` 폴백은 `PrebuiltMessages` 사용처(mail QA·OpenAI 호환 HTTP)가 모두 user 메시지를 갖고 있어 사실상 죽은 경로; `attachPersistedTails`는 copy-on-write(run_exec.go의 "in-place" 주석만 낡음).

### 4.1 polaris FTS 텍스트가 자기 추론을 스니펫·NextText로 되먹임 (수리)

- **증상**: `indexableText`(`gateway-go/internal/pipeline/polaris/store.go`)가 thinking 본문과 `[도구 name] {json}`을 한 FTS 텍스트로 넣었고, 그 스니펫이 `polaris(action=search)` 행과 **회상 근거 행**(`gateway-go/internal/pipeline/chat/recall/recall_evidence.go`의 `h.Snippet`/`h.NextText`)에 실렸다 — 요청서가 지목한 두 형상의 두 번째 발원지가 회상 꼬리 자체에 있었다.
- **더 날카로운 부작용**: Q→A 스티치의 `NextText`("답의 머리")가 저장된 TextContent의 머리 = 추론 모델에서는 **thinking의 머리**. LongMemEval로 측정해 넣은 답-스티치가 GLM 턴에서는 추론을 답으로 실어 `⏩ …`로 렌더했다.
- **수리**: 2-필드 인덱스 — 가시 텍스트(text·`[도구]`·tool_result)는 스니펫 소스, thinking은 `textsearch.Field{Hidden: true}`(매칭·점수만, 발췌 금지). 2026-07-05의 "thinking으로 찾을 수 있어야 한다" 의도는 유지된다. 원문 `Content`가 JSONL에 함께 있어 **로드 시 재계산으로 과거 행이 치유**된다(마이그레이션 없음, `TestLoadHealsThinkingOutOfPersistedTextContent`). 토큰 추정은 가시+숨김 합으로 유지.
- `polaris` search/expand 출력도 `<transcript-excerpt source="polaris" trust="untrusted">` 봉투(`toolport.WrapTranscriptExcerpt`, sessions와 공유).

### 4.2 게이트웨이 노트가 user 역할로 영속돼 사용자 말풍선으로 노출 (수리)

- **증상**: `persistTimeoutRemnant`·전달 미확인·중단 노트(`gateway-go/internal/pipeline/chat/run_delivery_failure.go`)는 모델의 다음 턴을 위해 `[SYSTEM: …]`/`**System:** …`를 **user 역할**로 트랜스크립트에 쓴다. 전사 RPC(`miniapp.sessions.transcript`·`chat.history`)의 표시 새니타이저는 링크 보강·tool_result·타임스탬프만 숨겨 이 노트가 "내가 쓴 말"로 렌더된다. 프로덕션 트랜스크립트 657개 중 **8개**가 해당(전달 미확인 5+2+1, 중단 1). 서브에이전트 완료 노트(`**System:** subagent completed…`)도 tool-results user 메시지의 text 블록으로 타 tool_result strip 뒤 같은 방식으로 남는다.
- **수리**: `toolport.IsSyntheticSystemNote` + `StripSyntheticSystemNotesForDisplay`를 두 RPC 체인에 배선(표시 전용, 저장 트랜스크립트 불변). 회상은 이 노트를 과거 대화 근거로 인용하지 않는다(`recall_evidence.go` 필터).

## 5. 범위 밖 / 남은 갭

- 엔진 커널·스케일·배율·난수·캐시: 변경 없음(요청서 §6).
- ~~mid-run 단계 갭~~ → **닫힘(3차)**: `gateway-go/internal/pipeline/chat/run_midrun_anchor.go` — 도구 결과가 마지막 메시지인 단계에 같은 앵커를 per-request **text 블록**으로 덧붙인다(역할 교대 문제 없음: Anthropic은 같은 user 메시지의 추가 블록, OpenAI 변환은 `tool…` 뒤 별도 `user` 턴 = 정확히 마지막 `<|user|>`). trailing-cache 훅 뒤에 실행돼 마커는 깨끗한 블록에 남는다. Ephemeral·kimi 제외, `DENEB_MIDRUN_ANCHOR=off` 킬스위치(모델 반응 라이브 관찰 전까지).
- **오염 가드(3차, R4의 제품측 등가물)**: `gateway-go/internal/pipeline/chat/response_contamination.go` — 최종 답의 하드 마커(특수 토큰·`<transcript-excerpt>`/`<recall-context>`·꼬리 헤더 에코·`[ctx] [assistant]`/`**[user]**`/`### Session:`/`source=session ref=` 행)를 코드펜스 밖에서 탐지, 한국어 접두사(한글 10자 이상)만 남기고 잘라 `(응답 뒷부분이 손상돼 잘라냈어요…)` 한 줄을 붙인다; 접두사가 없으면 빈-응답 경로와 같은 톤의 대체문. `run.contamination` 이벤트(마커·오프셋·한글 비율·붕괴 위치·salvaged)로 이 부류를 **센다**. 한글 비율 붕괴는 소프트 신호(로그만 — 영어 답은 정당할 수 있다). 사건의 907토큰 출력이었다면 `[ctx] [assistant]` 행에서 잘려 한국어 앞부분만 전달·영속됐을 것이고, 자기조건화(다음 턴이 오염문을 자기 말로 읽는 것)도 끊긴다.
- 리콜 벤치 골드셋은 레포 밖이다. `ExcerptText`는 회상 근거 렌더에 관여하지 않으므로(노트 추출 = `TextContent`) 회상 수치는 이 변경으로 움직이지 않아야 한다 — 움직이면 계측 부패를 먼저 의심한다.
