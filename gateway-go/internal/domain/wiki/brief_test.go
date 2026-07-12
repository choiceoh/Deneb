package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWikiBrief_MissingOrEmpty(t *testing.T) {
	if got := LoadWikiBrief(""); got != "" {
		t.Errorf("empty workspaceDir: got %q, want empty", got)
	}
	dir := t.TempDir()
	if got := LoadWikiBrief(dir); got != "" {
		t.Errorf("missing file: got %q, want empty", got)
	}
	if err := os.WriteFile(filepath.Join(dir, WikiBriefFileName), []byte("  \n\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadWikiBrief(dir); got != "" {
		t.Errorf("whitespace-only file: got %q, want empty", got)
	}
}

func TestLoadWikiBrief_ContentAndTruncation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, WikiBriefFileName),
		[]byte("\n태양광 단가 동향에 집중.\n뉴스레터류는 무시.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadWikiBrief(dir)
	if got != "태양광 단가 동향에 집중.\n뉴스레터류는 무시." {
		t.Errorf("trimmed content mismatch: %q", got)
	}

	// Oversize (multibyte-safe: rune budget, not bytes).
	big := strings.Repeat("가", maxWikiBriefRunes+100)
	if err := os.WriteFile(filepath.Join(dir, WikiBriefFileName), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	got = LoadWikiBrief(dir)
	if !strings.Contains(got, "이하 생략") {
		t.Error("oversize brief missing truncation marker")
	}
	if n := len([]rune(got)); n > maxWikiBriefRunes+100 {
		t.Errorf("truncated brief still oversized: %d runes", n)
	}
}

func TestWikiBriefSection(t *testing.T) {
	if got := WikiBriefSection(""); got != "" {
		t.Errorf("empty brief: got %q, want empty section", got)
	}
	got := WikiBriefSection("케이블 시세에 집중")
	for _, want := range []string{"운영자 위키 지침", "케이블 시세에 집중", "불변식이 우선"} {
		if !strings.Contains(got, want) {
			t.Errorf("section missing %q in %q", want, got)
		}
	}
}

// TestBuildWikiSynthesisPromptIncludesBriefSection pins the operator-steering
// injection point: a non-empty WIKI.md section must reach the dreamer's
// synthesis prompt verbatim, and an empty brief must add nothing.
func TestBuildWikiSynthesisPromptIncludesBriefSection(t *testing.T) {
	section := WikiBriefSection("ESS 안건은 반드시 프로젝트로 기록")
	prompt := buildWikiSynthesisPrompt("index", "history", "", section, "diary")
	if !strings.Contains(prompt, "ESS 안건은 반드시 프로젝트로 기록") {
		t.Error("synthesis prompt missing operator brief content")
	}
	if !strings.Contains(prompt, "운영자 위키 지침") {
		t.Error("synthesis prompt missing brief section heading")
	}

	plain := buildWikiSynthesisPrompt("index", "history", "", "", "diary")
	if strings.Contains(plain, "운영자 위키 지침") {
		t.Error("empty brief must not inject a section")
	}
}
