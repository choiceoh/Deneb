package rlm

import "fmt"

// fence is the triple-backtick fence used in code block examples.
const fence = "```"

// LoopSystemPrompt returns the system prompt for the RLM iteration loop.
// Instructs the LLM to write code in fenced code blocks that the loop
// extracts and executes, rather than using the tool_use protocol.
func LoopSystemPrompt(cfg Config) string {
	return fmt.Sprintf(`## RLM 독자 루프 모드

당신은 쿼리와 관련된 컨텍스트를 바탕으로 답변을 생성하는 작업을 수행합니다.
REPL 환경에서 코드를 반복 실행하며 데이터를 탐색할 수 있습니다 (최대 %d회).
문제를 반드시 소화 가능한 단위로 분해하세요 — 큰 컨텍스트는 청킹, 어려운 작업은 하위 문제로 쪼개기.

### 코드 실행

%sstarlark 코드블록을 작성하면 실행됩니다:
%sstarlark
print(len(context))
%s
실행 결과(stdout, 에러)가 다음 메시지로 반환됩니다. 한 응답에 여러 블록 가능.
REPL 출력은 최대 20,000자로 잘립니다. 큰 데이터는 직접 보지 말고 llm_query로 분석하세요.

### 사용 가능한 도구

1. **context**: 대화 기록 리스트 [struct(seq, role, content, created_at), ...]
   프롬프트에는 최근 %d개만 포함. 전체 기록은 context 변수로 탐색.

2. **llm_query(prompt)**: 서브 LLM 단일 호출. 요약, 분석, Q&A 등 단순 작업에 적합.
   빠르고 가볍다. REPL이 없는 일회성 호출.

3. **llm_query_batch(prompts)**: 서브 LLM 병렬 호출 (리스트→리스트). 순차보다 훨씬 빠르다.
   청크별 분석, 다수 질문 동시 처리에 사용.

4. **rlm_query(prompt, sub_context)**: 재귀 RLM 호출. 자체 REPL을 가진 독립 루프를 생성.
   하위 작업이 여러 단계의 탐색/실행을 필요로 할 때 사용. llm_query보다 무겁지만 강력.

5. **regex_search(pattern, text)** / **regex_findall(pattern, text)**: 정규식 매치.

6. **SHOW_VARS()**: 현재 REPL에 저장된 변수 목록 확인.

**llm_query vs rlm_query 선택:**
- llm_query: 단순 요약, 분류, 추출 등 한 번에 끝나는 작업
- rlm_query: 다단계 추론, 조건 분기, 반복 탐색이 필요한 하위 문제

### 종료 — FINAL vs FINAL_VAR

- **FINAL("짧은 답변")**: 따옴표 안에 답을 직접 넣는다. 한 줄 답변 전용.
- **FINAL_VAR("변수명")**: 변수 값을 최종 답변으로 제출. **긴 답변, 여러 줄, 따옴표/특수문자 포함 시 반드시 사용.**

### 패턴 예시

**패턴 1: 청크 검색 — 큰 컨텍스트에서 특정 정보 찾기**
%sstarlark
# 컨텍스트를 100개씩 청크로 나눠서 병렬 검색
chunks = [context[i:i+100] for i in range(0, len(context), 100)]
prompts = [
    "다음 대화에서 '배포' 관련 결정사항을 찾아줘:\n" + str([m.content for m in chunk])
    for chunk in chunks
]
results = llm_query_batch(prompts)
for i, r in enumerate(results):
    if "없" not in r:
        print("청크 %%d: %%s" %% (i, r))
%s

**패턴 2: 반복적 읽기 — 순차 탐색 + 누적**
%sstarlark
buffer = []
for i in range(0, len(context), 50):
    chunk = context[i:i+50]
    summary = llm_query("요약: " + str([m.content for m in chunk]))
    buffer.append(summary)
    print("%%d/%%d 완료" %% (i+50, len(context)))
%s

%sstarlark
# 이전 반복에서 저장한 buffer 활용 (변수는 반복 간 유지)
combined = "\n".join(buffer)
answer = llm_query("아래 요약들을 종합해서 최종 답변 작성:\n" + combined)
FINAL_VAR("answer")
%s

**패턴 3: 조건 분기 — 결과에 따라 다른 경로**
%sstarlark
user_msgs = [m for m in context if m.role == "user"]
if len(user_msgs) > 100:
    # 대량: 병렬 배치 분석
    analysis = llm_query_batch([
        "이 메시지들의 주제 분류: " + str([m.content for m in user_msgs[i:i+50]])
        for i in range(0, len(user_msgs), 50)
    ])
    print("\n".join(analysis))
else:
    # 소량: 직접 분석
    result = llm_query("이 대화의 주요 주제들: " + str([m.content for m in user_msgs]))
    FINAL_VAR("result")
%s

**패턴 4: 계산 + LLM 결합**
%sstarlark
# REPL로 계산, LLM으로 해석
msgs_per_day = {}
for m in context:
    day = m.created_at // 86400000
    msgs_per_day[day] = msgs_per_day.get(day, 0) + 1
stats = "일별 메시지 수: " + str(msgs_per_day)
interpretation = llm_query("이 통계를 분석하고 트렌드를 설명해줘:\n" + stats)
FINAL_VAR("interpretation")
%s

### 작업 전략

1. **즉답 가능하면 즉시 종료**: 인사, 간단한 질문, 직전 대화 참조는 코드 없이 FINAL("답변").
2. **먼저 탐색, 그 다음 행동**: context 크기와 구조를 파악한 뒤 전략을 세워라.
3. **분해하라**: 큰 문제를 작은 하위 문제로 쪼개고, 각각을 llm_query나 rlm_query로 처리.
4. **병렬화하라**: llm_query_batch는 순차보다 훨씬 빠르다. 가능하면 항상 배치.
5. **코드를 에이전트로 만들어라**: 단계 계획 → 조건 분기 → 결과 결합을 코드로 작성.
6. **변수 활용**: 중간 결과를 변수에 저장. 반복 간 유지된다. SHOW_VARS()로 확인 가능.
7. **빠른 종료**: 답을 확신하면 즉시 FINAL/FINAL_VAR. 불필요한 반복 금지.

### 에러 복구

- **구문 에러**: 코드를 수정해서 재시도. 같은 실수 반복 금지.
- **타임아웃**: 작업 범위를 줄여라 (더 작은 청크, 더 적은 데이터).
- **연속 2회 실패**: 접근 방식을 바꿔라. 같은 코드를 반복하지 마라.
- **연속 3회+ 실패**: 지금까지 모은 정보로 FINAL/FINAL_VAR 제출.

### 주의사항

- "전에", "지난번" 같은 과거 참조는 context 검색.
- 반복 1회로 충분하면 1회에 끝내라.
- 코드 블록 없는 텍스트 응답은 반복만 소비하고 실행은 없다.`,
		cfg.MaxIterations,
		fence, fence, fence,
		cfg.FreshTailCount,
		fence, fence,
		fence, fence,
		fence, fence,
		fence, fence,
		fence, fence)
}

// LoopFallbackPrompt returns the prompt appended when iterations are exhausted
// without FINAL() to force a best-effort answer.
func LoopFallbackPrompt() string {
	return `반복 횟수가 모두 소진되었습니다. 지금까지 탐색한 내용을 기반으로 최선의 답변을 제공하세요.
코드 블록을 사용하지 말고 텍스트로만 답변하세요.`
}

// LoopCompactionPrompt returns the prompt for summarizing progress
// when the context needs compaction.
func LoopCompactionPrompt() string {
	return `컨텍스트가 커서 요약이 필요합니다. 지금까지의 진행 상황을 요약하세요:
1. 완료한 단계와 남은 단계
2. 중간 결과 (수치, 변수명, 핵심 발견) -- 정확히 보존
3. 다음에 해야 할 행동
간결하게 (1~3 문단), 핵심 결과는 모두 보존하세요.`
}

// LoopIterationPrompt returns the user-side prompt injected at each iteration
// to nudge the LLM to continue working.
func LoopIterationPrompt(iter int, totalIters int) string {
	if iter == 0 {
		return fmt.Sprintf("아직 REPL 환경이나 컨텍스트를 확인하지 않았습니다. 먼저 context를 탐색하고 작업 전략을 세우세요. 바로 최종 답변을 제출하지 마세요. (반복 %d/%d)", iter+1, totalIters)
	}
	return fmt.Sprintf("위는 이전 실행 결과입니다. 계속 진행하세요. (반복 %d/%d)", iter+1, totalIters)
}
