package chat

import (
	"context"
	"strings"
	"testing"
)

func TestMutationOutcomeIsFailure(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		output string
		want   bool
	}{
		{"gmail send failure", "gmail", "발송 실패: 550 mailbox unavailable", true},
		{"gmail reply failure", "gmail", "답장 실패: timeout", true},
		{"gmail label add failure", "gmail", "라벨 추가 실패: not found", true},
		{"gmail success not flagged", "gmail", "✉️ 메일 발송 완료 → a@b.com (ID: 123)", false},
		{"wiki write failure", "wiki", "위키 페이지 쓰기 실패: disk full", true},
		{"wiki diary failure", "wiki", "일지 쓰기 실패: permission denied", true},
		{"wiki read success not flagged", "wiki", "# 문서 제목\n본문...", false},
		{"cron run failure", "cron", "❌ **job1** 실행 실패 (2s): boom", true},
		{"cron add success not flagged", "cron", "✅ 크론 작업 **job1** 추가 완료", false},
		{"gateway config save failure", "gateway", "설정 저장 실패: io error", true},
		{"gateway restart failure", "gateway", "재시작 신호 전송 실패: no pid", true},
		{"unknown tool never flagged", "exec", "발송 실패: whatever", false},
		{"read tool with 실패 not in table", "polaris", "검색 실패: x", false},
		{"empty output", "gmail", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mutationOutcomeIsFailure(c.tool, c.output); got != c.want {
				t.Fatalf("mutationOutcomeIsFailure(%q, %q) = %v, want %v", c.tool, c.output, got, c.want)
			}
		})
	}
}

func TestMutationFailureAnnotator_PrependsBannerOnFailure(t *testing.T) {
	out := MutationFailureAnnotator(context.Background(), "gmail", "발송 실패: boom")
	if !strings.HasPrefix(out, mutationFailureBanner) {
		t.Fatalf("expected banner prefix, got: %q", out)
	}
	if !strings.Contains(out, "발송 실패: boom") {
		t.Fatalf("original output must be preserved, got: %q", out)
	}
}

func TestMutationFailureAnnotator_NoOpOnSuccess(t *testing.T) {
	in := "✉️ 메일 발송 완료 → a@b.com (ID: 123)"
	if got := MutationFailureAnnotator(context.Background(), "gmail", in); got != in {
		t.Fatalf("success output must be unchanged, got: %q", got)
	}
}

func TestMutationFailureAnnotator_NoOpOnUnknownTool(t *testing.T) {
	in := "발송 실패: boom" // failure phrase but tool not in table
	if got := MutationFailureAnnotator(context.Background(), "exec", in); got != in {
		t.Fatalf("unknown tool output must be unchanged, got: %q", got)
	}
}

func TestMutationFailureAnnotator_Idempotent(t *testing.T) {
	ctx := context.Background()
	once := MutationFailureAnnotator(ctx, "wiki", "위키 페이지 쓰기 실패: x")
	twice := MutationFailureAnnotator(ctx, "wiki", once)
	if once != twice {
		t.Fatalf("annotator must be idempotent;\n once=%q\ntwice=%q", once, twice)
	}
	if strings.Count(twice, mutationFailureBanner) != 1 {
		t.Fatalf("banner must appear exactly once, got %d", strings.Count(twice, mutationFailureBanner))
	}
}

func TestMutationFailureAnnotator_WiredViaRegistry(t *testing.T) {
	// End-to-end through the PostProcessRegistry the same way Execute applies it.
	reg := NewToolRegistry()
	RegisterDefaultPostProcessors(reg)

	out := reg.postProcess.Apply(context.Background(), "gmail", "발송 실패: 550")
	if !strings.Contains(out, mutationFailureBanner) {
		t.Fatalf("registry-applied gmail failure should carry banner, got: %q", out)
	}

	ok := reg.postProcess.Apply(context.Background(), "gmail", "✉️ 메일 발송 완료")
	if strings.Contains(ok, mutationFailureBanner) {
		t.Fatalf("registry-applied gmail success must not carry banner, got: %q", ok)
	}
}

func TestIsMutationFailureResult(t *testing.T) {
	annotated := MutationFailureAnnotator(context.Background(), "gmail", "발송 실패: boom")
	if !isMutationFailureResult(annotated) {
		t.Fatal("annotated failure result must be detected for escalation")
	}
	if isMutationFailureResult("✉️ 메일 발송 완료") {
		t.Fatal("success result must not be detected as failure")
	}
	if isMutationFailureResult("") {
		t.Fatal("empty result must not be detected as failure")
	}
}

func TestMutationFailureError_StripsBanner(t *testing.T) {
	annotated := MutationFailureAnnotator(context.Background(), "gmail", "발송 실패: boom")
	if got := mutationFailureError(annotated); got != "발송 실패: boom" {
		t.Fatalf("got %q, want underlying failure", got)
	}
	if got := mutationFailureError("plain success"); got == "" {
		t.Fatal("fallback failure text must be non-empty")
	}
}

func TestMutationVerifyTools_Deterministic(t *testing.T) {
	got := mutationVerifyTools()
	want := []string{"cron", "gateway", "gmail", "wiki"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mutationVerifyTools not sorted/expected: got %v, want %v", got, want)
		}
	}
}
