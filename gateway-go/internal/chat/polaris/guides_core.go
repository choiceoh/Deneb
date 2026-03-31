package polaris

// Guide content constants for the polaris tool's "guides" action.
// Each guide is a practical cookbook for a Deneb subsystem.
// Focus: what it does, how to use it, what goes wrong.

const auroraGuide = `Aurora manages how conversation history is assembled into model context.

## 핵심 개념
대화가 길어지면 모든 메시지를 모델에 넣을 수 없다. Aurora가 토큰 예산(기본 100K) 안에서 메시지를 선별하고, 넘치면 자동으로 컴팩션(요약)한다.

## 사용자가 알아야 할 것
- 대화가 길어지면 자동으로 오래된 메시지가 요약됨 (자동 컴팩션, 85% 임계치)
- /compact 명령으로 수동 컴팩션 가능
- 컴팩션 전에 중요한 정보는 자동으로 메모리에 저장됨 (memory flush)

## Aurora 에이전트 도구
- aurora_grep: 대화 기록에서 키워드 검색
- aurora_describe: 메시지의 요약 계보 조회
- aurora_expand_query: 심층 검색 (~120초 소요, 정말 필요할 때만)

## 설정 (deneb.json)
- agents.defaults.context.tokenBudget: 토큰 예산 (기본 100K)
- agents.defaults.context.freshTailCount: 항상 보존할 최근 메시지 수 (기본 48)
- agents.defaults.compaction.memoryFlush.enabled: 컴팩션 전 메모리 저장 (기본 true)

## 문제 해결
- "대화 내용을 까먹었어요" → aurora_grep으로 검색하거나 memory(action=search)로 저장된 내용 확인
- "컨텍스트 오버플로우" → 자동 컴팩션이 최대 2회 재시도. 계속 실패하면 /compact 수동 실행
- aurora_expand_query는 120초 걸리므로 단순 키워드 검색에는 aurora_grep 사용`

const vegaGuide = `Vega는 프로젝트 지식 검색 엔진이다. 이메일, 문서, 메모 등을 인덱싱해서 검색한다.

## 검색 방식
- **BM25**: 키워드 정확 매칭 (짧은 검색어에 적합)
- **시맨틱**: 의미 기반 벡터 검색 (개념적 질문에 적합)
- **하이브리드**: 둘 다 합쳐서 최적 결과 (기본값)

자연어 질문을 넣으면 Vega가 자동으로 최적 모드를 선택한다.

## 사용법
- vega(action:'search', query:'세션 라이프사이클')
- vega(action:'dashboard') — 프로젝트 현황 대시보드
- vega(action:'brief') — 오늘의 요약 브리핑

## 설정
- GEMINI_API_KEY 환경변수 필요 (시맨틱 검색용 Gemini Embedding)
- 키 없으면 BM25 키워드 검색만 동작 (시맨틱 비활성화)

## 빌드
- make rust-vega: FTS만 (시맨틱 없음)
- make rust-dgx: 풀 프로덕션 (시맨틱 + CUDA)

## 문제 해결
- "검색 결과가 없어요" → GEMINI_API_KEY가 설정되었는지 확인. 없으면 키워드만 매칭됨
- "시맨틱 검색이 안 돼요" → make rust-dgx로 빌드했는지 확인`

const agentLoopGuide = `에이전트 루프는 사용자 메시지를 받아서 도구를 실행하고 응답하는 핵심 실행 사이클이다.

## 실행 흐름 (간략)
사용자 메시지 → 세션 큐잉 → 컨텍스트 조립 → LLM 호출 → 도구 실행 → 응답 전달
도구 호출이 있으면 결과를 다시 LLM에 넣어 반복 (최대 25턴).

## 기본 설정
- MaxTurns: 25 (도구 호출 반복 최대 횟수)
- Timeout: 10분 (전체 실행 시간 제한)
- MaxTokens: 8192 (LLM 응답 최대 토큰)
- 기본 모델: deneb.json의 agents.defaultModel로 설정

## 텔레그램 상태 이모지
- 👀 대기 중 → 🤔 생각 중 → 🔥 도구 실행 → ⚡ 웹 검색 → 👍 완료 / 😱 에러

## 도구 실행
- 독립적인 도구는 병렬 실행 (더 빠름)
- $ref로 도구 간 결과 전달 가능 (30초 타임아웃)
- 도구 출력이 64K자 넘으면 자동 트리밍 (앞뒤 보존, 중간 생략)

## 큐 모드 (메시지가 실행 중에 들어오면)
- collect: 모아서 한 번에 처리
- steer: 실행 중인 에이전트에 주입
- followup: 현재 실행 끝나면 다음으로 처리

## 문제 해결
- "25턴 넘어서 멈췄어요" → MaxTurns 한도. 복잡한 작업은 나눠서 요청
- "타임아웃 됐어요" → 10분 한도. 오래 걸리는 작업은 exec background 모드 사용
- "응답이 잘렸어요" → MaxTokens 8192 한도. 긴 응답은 나눠서 받기`

const compactionGuide = `컴팩션은 대화가 길어졌을 때 오래된 메시지를 요약해서 컨텍스트 공간을 확보하는 기능이다.

## 작동 방식
1. 대화 토큰이 예산의 85%를 넘으면 자동 실행
2. 오래된 메시지들을 요약본으로 교체
3. 최근 메시지는 항상 보존 (기본 8개)
4. 요약은 세션 기록(JSONL)에 영구 저장

## 수동 실행
/compact — 즉시 컴팩션
/compact 코드 변경사항 위주로 — 포커스 지시 가능

## 설정 (deneb.json: agents.defaults.compaction)
- model: 요약에 사용할 모델 지정 가능
- reserveTokensFloor: 컴팩션 전 예약 토큰 (기본 20000)
- memoryFlush.enabled: 컴팩션 전 중요 정보 메모리 저장 (기본 true)

## 컴팩션 vs 프루닝
- 컴팩션: 요약 후 영구 저장 (재시작 후에도 유지)
- 프루닝: 오래된 도구 결과를 메모리에서만 트리밍 (임시)

## 문제 해결
- "짧은 대화인데 컴팩션이 안 돼요" → 최근 8개 메시지는 항상 보호됨. 대화가 충분히 길어야 함
- "컨텍스트 오버플로우" → 자동 컴팩션 최대 2회 재시도. 계속 실패하면 /compact 수동 실행
- "메모리 플러시가 안 돼요" → 워크스페이스가 읽기 전용이면 스킵됨`
