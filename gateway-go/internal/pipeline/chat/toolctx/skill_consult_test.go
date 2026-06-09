package toolctx

import (
	"context"
	"testing"
)

func TestSkillConsultLog_DrainNew_dedupAndHighWater(t *testing.T) {
	var nilLog *SkillConsultLog
	if got := nilLog.DrainNew(); got != nil {
		t.Fatalf("nil log DrainNew = %v, want nil", got)
	}
	nilLog.Add("x") // nil-safe no-op — must not panic

	l := NewSkillConsultLog()
	if got := l.DrainNew(); len(got) != 0 {
		t.Fatalf("empty DrainNew = %v, want empty", got)
	}

	l.Add("alpha")
	l.Add("beta")
	l.Add("alpha") // duplicate within the same window collapses
	l.Add("")      // empty name ignored
	got := l.DrainNew()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("DrainNew = %v, want [alpha beta]", got)
	}

	// High-water mark: already-drained entries are not returned again.
	if got := l.DrainNew(); len(got) != 0 {
		t.Fatalf("second DrainNew = %v, want empty (high-water)", got)
	}

	// New consults after a drain are returned on the next drain only.
	l.Add("gamma")
	if got := l.DrainNew(); len(got) != 1 || got[0] != "gamma" {
		t.Fatalf("third DrainNew = %v, want [gamma]", got)
	}
}

func TestSkillConsultLog_ContextRoundTrip(t *testing.T) {
	if SkillConsultLogFromContext(context.Background()) != nil {
		t.Fatal("absent log should read as nil")
	}
	l := NewSkillConsultLog()
	ctx := WithSkillConsultLog(context.Background(), l)
	if SkillConsultLogFromContext(ctx) != l {
		t.Fatal("round-trip did not return the same log")
	}
}
