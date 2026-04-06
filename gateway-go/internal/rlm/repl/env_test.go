package repl

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testMessages() []MessageEntry {
	return []MessageEntry{
		{Seq: 1, Role: "user", Content: "안녕하세요", CreatedAt: 1712000000000},
		{Seq: 2, Role: "assistant", Content: "안녕하세요! 무엇을 도와드릴까요?", CreatedAt: 1712000001000},
		{Seq: 3, Role: "user", Content: "프로젝트 상태 알려줘", CreatedAt: 1712000002000},
		{Seq: 4, Role: "assistant", Content: "현재 프로젝트는 진행 중입니다.", CreatedAt: 1712000003000},
		{Seq: 5, Role: "user", Content: "magic number is 42", CreatedAt: 1712000004000},
	}
}

func noopLLMQuery(_ context.Context, prompt string) (string, error) {
	return fmt.Sprintf("LLM response to: %s", prompt[:min(len(prompt), 50)]), nil
}

func newTestEnv(t *testing.T) *Env {
	t.Helper()
	return NewEnv(context.Background(), EnvConfig{
		Messages:   testMessages(),
		LLMQueryFn: noopLLMQuery,
		Timeout:    5 * time.Second,
	})
}

func TestExecute_BasicPrint(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`print("hello world")`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "hello world") {
		t.Errorf("expected 'hello world' in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_ContextAccess(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`print(len(context))`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "5") {
		t.Errorf("expected '5' in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_ContextSlicing(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
first = context[0]
print(first.role)
print(first.content)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "user") {
		t.Errorf("expected 'user' in stdout, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "안녕하세요") {
		t.Errorf("expected '안녕하세요' in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_ListComprehension(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
user_msgs = [m for m in context if m.role == "user"]
print(len(user_msgs))
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "3") {
		t.Errorf("expected '3' (user messages) in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_KeywordSearch(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
matches = [m for m in context if "프로젝트" in m.content]
for m in matches:
    print(m.seq, m.content)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "프로젝트") {
		t.Errorf("expected '프로젝트' in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_VariablePersistence(t *testing.T) {
	env := newTestEnv(t)

	// First execution: set variable
	r1 := env.Execute(`x = 42`)
	if r1.Error != "" {
		t.Fatalf("exec 1 error: %s", r1.Error)
	}

	// Second execution: read variable from previous
	r2 := env.Execute(`print(x)`)
	if r2.Error != "" {
		t.Fatalf("exec 2 error: %s", r2.Error)
	}
	if !strings.Contains(r2.Stdout, "42") {
		t.Errorf("expected '42' in stdout (variable persistence), got: %q", r2.Stdout)
	}
}

func TestExecute_FINAL(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`FINAL("the answer is 42")`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.FinalAnswer == nil {
		t.Fatal("expected final answer to be set")
	}
	if *result.FinalAnswer != "the answer is 42" {
		t.Errorf("expected 'the answer is 42', got: %q", *result.FinalAnswer)
	}
}

func TestExecute_FINAL_VAR(t *testing.T) {
	env := newTestEnv(t)

	// Set variable, then use FINAL_VAR
	r1 := env.Execute(`answer = "computed result"`)
	if r1.Error != "" {
		t.Fatalf("exec 1 error: %s", r1.Error)
	}

	r2 := env.Execute(`FINAL_VAR("answer")`)
	if r2.Error != "" {
		t.Fatalf("exec 2 error: %s", r2.Error)
	}
	if r2.FinalAnswer == nil {
		t.Fatal("expected final answer to be set")
	}
	if *r2.FinalAnswer != "computed result" {
		t.Errorf("expected 'computed result', got: %q", *r2.FinalAnswer)
	}
}

func TestExecute_LLMQuery(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
response = llm_query("summarize this")
print(response)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "LLM response to") {
		t.Errorf("expected LLM response in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_RegexFindall(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
text = "price is 42 and quantity is 7"
nums = regex_findall("[0-9]+", text)
print(nums)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "42") || !strings.Contains(result.Stdout, "7") {
		t.Errorf("expected numbers in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_RegexSearch(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`
text = "magic number is 42"
match = regex_search("number is ([0-9]+)", text)
print(match)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "number is 42") {
		t.Errorf("expected match in stdout, got: %q", result.Stdout)
	}
}

func TestExecute_SyntaxError(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`def foo(`)
	if result.Error == "" {
		t.Fatal("expected syntax error")
	}
}

func TestExecute_RuntimeError(t *testing.T) {
	env := newTestEnv(t)
	result := env.Execute(`x = 1 / 0`)
	if result.Error == "" {
		t.Fatal("expected runtime error")
	}
	if !strings.Contains(result.Error, "division") {
		t.Errorf("expected division error, got: %q", result.Error)
	}
}

func TestExecute_ErrorRecovery(t *testing.T) {
	env := newTestEnv(t)

	// First: error
	r1 := env.Execute(`x = 1 / 0`)
	if r1.Error == "" {
		t.Fatal("expected error")
	}

	// Second: should still work (env not corrupted)
	r2 := env.Execute(`print("recovered")`)
	if r2.Error != "" {
		t.Fatalf("unexpected error after recovery: %s", r2.Error)
	}
	if !strings.Contains(r2.Stdout, "recovered") {
		t.Errorf("expected 'recovered' in stdout, got: %q", r2.Stdout)
	}
}

func TestExecute_Timeout(t *testing.T) {
	env := NewEnv(context.Background(), EnvConfig{
		Messages:   testMessages(),
		LLMQueryFn: noopLLMQuery,
		Timeout:    100 * time.Millisecond,
	})

	result := env.Execute(`
x = 0
for i in range(100000000):
    x = x + 1
`)
	if result.Error == "" {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(result.Error, "timed out") && !strings.Contains(result.Error, "cancel") {
		t.Errorf("expected timed out/cancel error, got: %q", result.Error)
	}
}

func TestExecute_ShowVars(t *testing.T) {
	env := newTestEnv(t)
	env.Execute(`my_var = "hello"`)
	env.Execute(`another = 123`)

	result := env.Execute(`
vars = SHOW_VARS()
print(vars)
`)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "my_var") {
		t.Errorf("expected 'my_var' in SHOW_VARS output, got: %q", result.Stdout)
	}
}

// --- Preprocess tests ---

func TestPreprocess_FString(t *testing.T) {
	tests := []struct {
		input    string
		contains string // expected substring in output
		excludes string // should not contain this
	}{
		{
			input:    `print(f"count: {n}")`,
			contains: `%`,
			excludes: `f"`,
		},
		{
			input:    `print(f"x={x}, y={y}")`,
			contains: `%`,
		},
		{
			input:    `x = f'{name} is {age}'`,
			contains: `%`,
			excludes: `f'`,
		},
		{
			input:    `print(f"ratio: {val:.2f}")`,
			contains: `%.2f`,
		},
	}

	for _, tt := range tests {
		result := Preprocess(tt.input)
		if tt.contains != "" && !strings.Contains(result, tt.contains) {
			t.Errorf("Preprocess(%q): expected to contain %q, got: %q", tt.input, tt.contains, result)
		}
		if tt.excludes != "" && strings.Contains(result, tt.excludes) {
			t.Errorf("Preprocess(%q): should not contain %q, got: %q", tt.input, tt.excludes, result)
		}
	}
}

func TestPreprocess_ImportRemoval(t *testing.T) {
	code := `import re
import json
from collections import Counter
x = 42`

	result := Preprocess(code)
	if strings.Contains(result, "import re") {
		t.Errorf("import re should be removed, got: %q", result)
	}
	if strings.Contains(result, "import json") {
		t.Errorf("import json should be removed, got: %q", result)
	}
	if !strings.Contains(result, "x = 42") {
		t.Errorf("non-import code should be preserved, got: %q", result)
	}
}

func TestPreprocess_NoFalsePositive(t *testing.T) {
	// "if", "def" contain 'f' but should not be treated as f-strings
	code := `if x == "hello":
    print("world")
def foo():
    return "bar"`

	result := Preprocess(code)
	if result != code {
		t.Errorf("should not modify non-f-string code\ninput:  %q\noutput: %q", code, result)
	}
}

func TestPreprocess_EscapedBraces(t *testing.T) {
	code := `print(f"dict: {{key: {val}}}")`
	result := Preprocess(code)
	// Should handle {{ as literal { in format string
	if strings.Contains(result, "{{") {
		t.Logf("escaped braces preserved (ok if format string has {): %q", result)
	}
}

// --- Integration: f-string preprocess + Starlark execution ---

func TestExecute_FStringIntegration(t *testing.T) {
	env := newTestEnv(t)

	// LLM would naturally write this Python code with f-strings
	result := env.Execute(`
n = len(context)
print(f"Total messages: {n}")
`)
	if result.Error != "" {
		t.Fatalf("f-string integration failed: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "Total messages: 5") {
		t.Errorf("expected 'Total messages: 5', got: %q", result.Stdout)
	}
}

func TestExecute_ImportIntegration(t *testing.T) {
	env := newTestEnv(t)

	// LLM writes import + uses builtin regex instead
	result := env.Execute(`
import re
text = "hello 123 world 456"
nums = regex_findall("[0-9]+", text)
print(nums)
`)
	if result.Error != "" {
		t.Fatalf("import integration failed: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "123") {
		t.Errorf("expected '123' in output, got: %q", result.Stdout)
	}
}

func TestExecute_FullRLMWorkflow(t *testing.T) {
	env := newTestEnv(t)

	// Step 1: Explore context
	r1 := env.Execute(`
print(f"Context has {len(context)} messages")
user_msgs = [m for m in context if m.role == "user"]
print(f"User messages: {len(user_msgs)}")
`)
	if r1.Error != "" {
		t.Fatalf("step 1 error: %s", r1.Error)
	}

	// Step 2: Search for specific content
	r2 := env.Execute(`
matches = [m for m in context if "magic" in m.content]
for m in matches:
    print(f"Found at seq {m.seq}: {m.content}")
`)
	if r2.Error != "" {
		t.Fatalf("step 2 error: %s", r2.Error)
	}
	if !strings.Contains(r2.Stdout, "magic number is 42") {
		t.Errorf("expected magic number match, got: %q", r2.Stdout)
	}

	// Step 3: Use LLM query and produce final answer
	r3 := env.Execute(`
chunk = str([m.content for m in matches])
analysis = llm_query("Extract the number from: " + chunk)
FINAL(f"The magic number is 42, confirmed by LLM: {analysis}")
`)
	if r3.Error != "" {
		t.Fatalf("step 3 error: %s", r3.Error)
	}
	if r3.FinalAnswer == nil {
		t.Fatal("expected final answer")
	}
	if !strings.Contains(*r3.FinalAnswer, "42") {
		t.Errorf("expected '42' in final answer, got: %q", *r3.FinalAnswer)
	}
}
