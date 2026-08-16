package genesis

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"
)

func TestClassifySurfaceReturnsTierByPathPattern(t *testing.T) {
	cases := []struct {
		target   string
		wantName string
		wantTier string
	}{
		{"~/.deneb/skills/contract-review/SKILL.md", "skill-body", surfaces.SurfaceTierAutoApply},
		{"workspace/HEARTBEAT.md", "heartbeat-instructions", surfaces.SurfaceTierProposeOnly},
		{"workspace/AGENTS.md", "workspace-context", surfaces.SurfaceTierProposeOnly},
		{"gateway-go/internal/pipeline/chat/prompt/prompt_cache.go", "prompt-cache-path", surfaces.SurfaceTierForbidden},
		{".github/dependabot.yml", "security-owned", surfaces.SurfaceTierForbidden},
		{"gateway-go/internal/runtime/mailflow/mail_counterparty.go", "gateway-source", surfaces.SurfaceTierAutoApply},
		{"", "undeclared", surfaces.SurfaceTierProposeOnly},
	}
	for _, tc := range cases {
		got := surfaces.ClassifySurface(tc.target)
		if got.Name != tc.wantName || got.Tier != tc.wantTier {
			t.Errorf("ClassifySurface(%q) = (%s, %s), want (%s, %s)", tc.target, got.Name, got.Tier, tc.wantName, tc.wantTier)
		}
	}
}

// A forbidden target must reject the candidate at record time; allowed
// targets annotate the record with the most restrictive tier.
func TestRecordSelfCorrectionCandidate_RejectsForbiddenSummarizesMixedAndAutoApplySurfaces(t *testing.T) {
	tr := newTestTracker(t)

	_, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title:       "cache tweak",
		TargetFiles: []string{"internal/pipeline/chat/cache_breakpoints.go"},
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden surface") {
		t.Fatalf("forbidden surface must reject the candidate, got %v", err)
	}

	rec, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title:       "skill + context fix",
		TargetFiles: []string{"skills/x/SKILL.md", "workspace/TOOLS.md"},
	})
	if err != nil {
		t.Fatalf("allowed surfaces should record: %v", err)
	}
	if rec.Surface != surfaces.SurfaceTierProposeOnly {
		t.Fatalf("mixed targets must summarize to the most restrictive tier, got %q", rec.Surface)
	}

	auto, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title:       "skill only",
		TargetFiles: []string{"skills/x/SKILL.md"},
	})
	if err != nil {
		t.Fatalf("skill-body target should record: %v", err)
	}
	if auto.Surface != surfaces.SurfaceTierAutoApply {
		t.Fatalf("skill-body-only candidate should summarize auto-apply, got %q", auto.Surface)
	}
}
