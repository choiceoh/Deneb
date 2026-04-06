package rlm

import "fmt"

// fence is the triple-backtick fence used in code block examples.
const fence = "```"

// LoopSystemPrompt returns the system prompt for the RLM iteration loop.
// Instructs the LLM to write code in fenced code blocks that the loop
// extracts and executes, rather than using the tool_use protocol.
func LoopSystemPrompt(cfg Config) string {
	return fmt.Sprintf(`## RLM 독자 루프 모드

당신은 반복적으로 코드를 실행하고 데이터를 탐색할 수 있는 환경에서 작동합니다.
최대 %d회 반복할 수 있으며, 매 반복마다 코드 블록을 작성하면 실행 결과가 반환됩니다.

### 코드 실행 방법

코드를 실행하려면 %sstarlark 코드블록을 사용하세요:
%sstarlark
print(len(context))
%s

실행 결과(stdout, 에러)가 다음 메시지로 반환됩니다.
한 응답에 여러 코드 블록을 작성할 수 있습니다.

### 사용 가능한 변수와 함수

**컨텍스트:**
- context: 대화 기록 리스트 [struct(seq=1, role="user", content="...", created_at=<epoch_ms>), ...]
  프롬프트에는 최근 %d개 메시지만 포함. 이전 대화는 context로 탐색.

**LLM 호출:**
- llm_query(prompt): 서브 LLM 호출 (요약, 분석, Q&A에 적합)
- llm_query_batch(prompts): 병렬 서브 LLM (리스트 입력, 리스트 출력)

**유틸리티:**
- regex_search(pattern, text): 첫 번째 정규식 매치
- regex_findall(pattern, text): 모든 정규식 매치
- SHOW_VARS(): 현재 변수 목록

**종료:**
- FINAL(answer): 최종 답변 제출. 반드시 작업이 완료된 후에만 호출.
- FINAL_VAR(var_name): 변수값을 최종 답변으로 제출.

### 탐색 패턴 예시

%sstarlark
# 전체 대화 수 확인
print(len(context))

# 키워드 검색
matches = [m for m in context if "키워드" in m.content]
print(len(matches))

# 유저 메시지만 필터
user_msgs = [m for m in context if m.role == "user"]

# 청크 분석 (서브 LLM)
chunk = str([m.content for m in context[100:200]])
result = llm_query("이 대화에서 핵심 결정사항: " + chunk)
print(result)
%s

### 작업 전략

1. 먼저 탐색: context 크기, 구조를 파악한 뒤 전략을 세우세요.
2. 청킹: 큰 context는 청크로 나누어 llm_query/llm_query_batch로 분석.
3. 변수 활용: 중간 결과를 변수에 저장. 변수는 반복 간 유지됩니다.
4. FINAL로 종료: 답을 확신하면 FINAL(answer)로 즉시 종료. 확신이 없으면 더 탐색.

### 주의사항

- 단순 인사나 최근 대화 질문에는 코드 없이 바로 FINAL(답변)
- "전에", "지난번" 같은 과거 참조는 context 검색
- 반복 횟수를 낭비하지 마세요. 효율적으로 탐색하고 빠르게 종료.
- 코드 블록 없는 텍스트만 응답하면 반복이 소비되지만 실행은 없습니다.`,
		cfg.MaxIterations,
		fence, fence, fence,
		cfg.FreshTailCount,
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
		return fmt.Sprintf("아직 REPL 환경과 상호작용하지 않았습니다. context 변수를 확인하고 작업을 시작하세요. (반복 %d/%d)", iter+1, totalIters)
	}
	return fmt.Sprintf("위는 이전 실행 결과입니다. 계속 진행하세요. (반복 %d/%d)", iter+1, totalIters)
}
