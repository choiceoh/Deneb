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

### R4 — 엔진 측 가드: 이 레포 범위 밖.

## 3. 수용 기준 — A1 정정

요청서 A1("동일 id에 R1만 적용한 재생")은 자기모순이다: 지시를 끼워 넣으면 id가 바뀐다. 대조 실험으로 다시 쓴다.

> **A1′.** 같은 원본(캡처된 프롬프트 텍스트 또는 id 시퀀스)에서 두 arm을 조립한다. arm A = 원본 그대로(= **A3 대조군**), arm B = 마지막 assistant 마커 직전에 앵커 블록만 삽입. **통제 검증**: `sha256(A[:off]) == sha256(B[:off])` 이고 `B == A[:off] + 앵커 + A[off:]`임을 디코드 전에 단언. 그 위에서 T=1·seed 7·max 2048·캐시 미사용으로 두 arm을 디코드한다.
> - A1′ 통과 = arm B에서 언어 붕괴(`collapse_at<0`)·툴 마크업(`tool_markup_hits=0`) 소멸, 한글 비율 ≥ 0.5
> - **A2** = arm B 초기 8토큰에 `Follow`/`User`/`Simple`/`Context` 류 미출현
> - **A3** = arm A에서 오염 재현 (수정이 원인을 건드렸음을 증명)

**결정적 절반(레포 안)**: `TestTailAnchorIsAControlledSuffixOfTheWireMessage` (`gateway-go/internal/pipeline/chat/run_tail_anchor_test.go`)가 게이트웨이 조립에서 같은 통제 속성(접두사 해시 일치·엄격한 접미사·바이트 안정)을 단언한다. `TestTelemachusReplayAnchorMatchesGateway`가 스크립트의 앵커 텍스트를 게이트웨이 상수에 고정한다.

**엔진 절반(레포 밖 데이터)**: `scripts/dev/telemachus_ab_replay.py` — 원본 파일을 받아 두 arm을 만들고 통제 검증 후 디코드·채점한다. 출력은 해시·카운터 JSON만; 원문 덤프는 `--dump-dir`이 저장소 밖일 때만.

```bash
python3 scripts/dev/telemachus_ab_replay.py --engine http://<engine> --model glm-5.3-flash --prompt-file /data/incidents/stream_0043.prompt.txt --cache-salt
```

id 모드(`--ids-file`)는 엔진 `/tokenize`가 `tokens` 리스트를 돌려줄 때만 가능하다(현행 Go 클라이언트는 `count`만 읽는다).

## 4. 범위 밖 / 남은 갭
- 엔진 커널·스케일·배율·난수·캐시: 변경 없음(요청서 §6).
- **mid-run 단계**(도구 결과가 마지막 메시지일 때)에는 앵커가 마지막 user 메시지에 남아 절대적 마지막은 아니다. per-request `BeforeAPICall` 꼬리 메시지로 옮기면 더 강하지만, Anthropic 역할 교대·trailing cache 마커와의 상호작용을 따로 설계해야 한다. 사건 원본은 첫 생성 단계(꼬리 = 전달정책 → assistant 마커)였으므로 이번 수정이 사건 형상을 정확히 덮는다.
- 리콜 벤치 골드셋은 레포 밖이다. `ExcerptText`는 회상 근거 렌더에 관여하지 않으므로(노트 추출 = `TextContent`) 회상 수치는 이 변경으로 움직이지 않아야 한다 — 움직이면 계측 부패를 먼저 의심한다.
