package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

func TestNewCalendarGlanceFunc_NilWhenNoSource(t *testing.T) {
	if fn := NewCalendarGlanceFunc(&toolctx.CalendarDeps{}); fn != nil {
		t.Error("expected nil func when no calendar source is wired")
	}
	if fn := NewCalendarGlanceFunc(nil); fn != nil {
		t.Error("expected nil func for nil deps")
	}
}

func TestNewCalendarGlanceFunc_BuildsAndFreezesPerDay(t *testing.T) {
	// Reset the global day cache so this test is deterministic regardless of
	// other tests in the package that may have populated it.
	calGlanceCache.mu.Lock()
	calGlanceCache.built = false
	calGlanceCache.date = ""
	calGlanceCache.value = ""
	calGlanceCache.mu.Unlock()

	store, err := localcal.New(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatalf("localcal.New: %v", err)
	}
	if _, err := store.Create(localcal.CreateInput{Summary: "내부 회의", Start: time.Now().Add(2 * time.Hour)}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fn := NewCalendarGlanceFunc(&toolctx.CalendarDeps{Local: store})
	if fn == nil {
		t.Fatal("expected non-nil func")
	}

	out := fn(context.Background(), "client:main", "Asia/Seoul")
	if !strings.Contains(out, "내부 회의") {
		t.Fatalf("glance missing seeded event:\n%s", out)
	}

	// Add another event, then call again the same day: the frozen cache must
	// return the original value (byte-stable dynamic block within a day).
	if _, err := store.Create(localcal.CreateInput{Summary: "추가 회의", Start: time.Now().Add(3 * time.Hour)}); err != nil {
		t.Fatalf("seed2: %v", err)
	}
	out2 := fn(context.Background(), "client:main", "Asia/Seoul")
	if out2 != out {
		t.Errorf("expected frozen cached glance within a day:\nfirst:\n%s\nsecond:\n%s", out, out2)
	}
	if strings.Contains(out2, "추가 회의") {
		t.Errorf("frozen cache should not reflect a post-build event:\n%s", out2)
	}
}
