package schedule

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
)

func cronTestDeps(t *testing.T) *tooldeps.ChronoDeps {
	t.Helper()
	svc := cron.NewService(
		cron.ServiceConfig{StorePath: filepath.Join(t.TempDir(), "cron.json")},
		nil,
		slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	)
	return &tooldeps.ChronoDeps{Service: svc}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// cron's store replaces by ID, so an add that reuses an existing name discards
// the operator's automation — schedule, command, delivery config, history —
// while the result still says 추가. calendar create names the slot it collides
// with and wiki write names the sections it dropped; cron said nothing on the
// higher-stakes surface.
func TestCronAddReportsTheJobItReplaced(t *testing.T) {
	d := cronTestDeps(t)
	ctx := context.Background()

	first, err := cronAdd(ctx, d, "weekly-audit", "0 6 * * 6", "주간 참조 자가감사", nil, nil, cronToolOpts{})
	if err != nil {
		t.Fatalf("seed add: %v", err)
	}
	if strings.Contains(first, "⚠️") {
		t.Fatalf("a fresh add warned about a replacement:\n%s", first)
	}

	second, err := cronAdd(ctx, d, "weekly-audit", "0 9 * * 1", "완전히 다른 명령", nil, nil, cronToolOpts{})
	if err != nil {
		t.Fatalf("replacing add: %v", err)
	}
	if !strings.Contains(second, "⚠️") || !strings.Contains(second, "교체") {
		t.Errorf("a replacing add reported a plain 추가:\n%s", second)
	}
	// The notice has to name what changed, or it cannot be acted on.
	if !strings.Contains(second, "스케줄") || !strings.Contains(second, "명령") {
		t.Errorf("notice did not name the schedule and command change:\n%s", second)
	}
	if !strings.Contains(second, "주간 참조 자가감사") {
		t.Errorf("notice did not quote the command it discarded:\n%s", second)
	}
	if !strings.Contains(second, "cron update") {
		t.Errorf("notice did not point at the deliberate-edit path:\n%s", second)
	}
}

// Re-adding an identical job is not a loss worth naming, but the ID collision
// still is: silently reporting 추가 would hide that no new job appeared.
func TestCronAddOnIdenticalJobStillNamesTheCollision(t *testing.T) {
	d := cronTestDeps(t)
	ctx := context.Background()

	if _, err := cronAdd(ctx, d, "daily", "0 9 * * *", "뉴스 확인", nil, nil, cronToolOpts{}); err != nil {
		t.Fatalf("seed add: %v", err)
	}
	again, err := cronAdd(ctx, d, "daily", "0 9 * * *", "뉴스 확인", nil, nil, cronToolOpts{})
	if err != nil {
		t.Fatalf("identical add: %v", err)
	}
	if !strings.Contains(again, "교체") {
		t.Errorf("an ID collision was reported as a plain add:\n%s", again)
	}
	// Nothing changed, so the notice must not invent a diff.
	if strings.Contains(again, "스케줄 \"") || strings.Contains(again, "명령 \"") {
		t.Errorf("notice listed changes for an identical job:\n%s", again)
	}
}

// A genuinely new job stays quiet — the warning must not fire on every add.
func TestCronAddStaysQuietForANewJob(t *testing.T) {
	d := cronTestDeps(t)
	out, err := cronAdd(context.Background(), d, "brand-new", "0 7 * * *", "새 작업", nil, nil, cronToolOpts{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if strings.Contains(out, "⚠️") || strings.Contains(out, "교체") {
		t.Errorf("a new job warned:\n%s", out)
	}
}
