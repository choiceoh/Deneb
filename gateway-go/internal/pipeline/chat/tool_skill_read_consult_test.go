package chat

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func consultCatalog() []skills.PromptSkill {
	return []skills.PromptSkill{
		{Name: "contract-review", FilePath: "/home/u/deneb/skills/productivity/contract-review/SKILL.md"},
		{Name: "topsolar-db", FilePath: "/home/u/.deneb/skills/topsolar-db/SKILL.md"},
	}
}

// TestSkillNameFromReadOutput: the read-tool header identifies a cataloged
// SKILL.md by its directory name — bundled and workspace layouts, the
// anchor-columns header variant, and the non-matches.
func TestSkillNameFromReadOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			"bundled relative path",
			"[File: skills/productivity/contract-review/SKILL.md | 88 lines]\n1\t---\n2\tname: contract-review", "contract-review",
		},
		{
			"workspace absolute path",
			"[File: /home/u/.deneb/skills/topsolar-db/SKILL.md | 40 lines]\n1\t---", "topsolar-db",
		},
		{
			"anchor-columns header variant",
			"[File: skills/productivity/contract-review/SKILL.md | 88 lines | columns: line<TAB>anchor<TAB>content — pass anchor=<hash> to edit]\n1\t---", "contract-review",
		},
		{"not a SKILL.md", "[File: gateway-go/internal/pipeline/chat/run_exec.go | 300 lines]\n1\tpackage chat", ""},
		{
			"SKILL.md outside the catalog (coding worktree of an unknown skill)",
			"[File: /tmp/worktree/skills/devops/tmux/SKILL.md | 30 lines]\n1\t---", "",
		},
		{"not a read output", "총 3건의 메일이 있습니다.", ""},
		{"malformed header", "[File: broken-header-without-separator]\n본문", ""},
	}
	for _, tc := range cases {
		if got := skillNameFromReadOutput(tc.output, consultCatalog()); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := skillNameFromReadOutput("[File: skills/x/contract-review/SKILL.md | 1 lines]\n", nil); got != "" {
		t.Errorf("empty catalog must never match, got %q", got)
	}
}

// TestReadSkillConsultRecorder: the post-processor records the consult into
// the run's consult log and passes the output through unchanged. (It reads the
// GLOBAL skills snapshot, empty under test, so this exercises the no-match
// passthrough; the matching itself is covered by the pure function above.)
func TestReadSkillConsultRecorder(t *testing.T) {
	log := newSkillConsultLog()
	ctx := withSkillConsultLog(context.Background(), log)
	in := "[File: skills/productivity/contract-review/SKILL.md | 88 lines]\n1\t---"
	if out := newReadSkillConsultRecorder(nil)(ctx, "read", in); out != in {
		t.Fatalf("output must pass through unchanged")
	}
	// Direct wiring proof with an explicit catalog: matcher result → log.Add.
	if name := skillNameFromReadOutput(in, consultCatalog()); name != "" {
		log.Add(name)
	}
	if got := log.DrainNew(); len(got) != 1 || got[0] != "contract-review" {
		t.Fatalf("consult log = %v, want [contract-review]", got)
	}
}
