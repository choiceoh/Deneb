package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// fakeWriter records the mirror calls the handler makes and can force Push to
// fail (to prove the local write still succeeds).
type fakeWriter struct {
	pushed   []string // local ids pushed
	removed  []string // local ids removed
	mirrored map[string]struct{}
	pushErr  bool
}

func (f *fakeWriter) Push(_ context.Context, localID string, _ calendar.Event) error {
	f.pushed = append(f.pushed, localID)
	if f.pushErr {
		return errors.New("google down")
	}
	return nil
}

func (f *fakeWriter) Remove(_ context.Context, localID string) error {
	f.removed = append(f.removed, localID)
	return nil
}

func (f *fakeWriter) MirroredGoogleIDs() map[string]struct{} { return f.mirrored }

func depsWithWriter(t *testing.T, w CalendarWriter) (CalendarDeps, *localcal.Store) {
	t.Helper()
	deps, store := calDepsWithLocal(t, nil)
	deps.Writer = func() (CalendarWriter, error) { return w, nil }
	return deps, store
}

func TestCalendarCreate_MirrorsToWriter(t *testing.T) {
	w := &fakeWriter{}
	deps, _ := depsWithWriter(t, w)
	resp := calendarCreate(deps)(authedCtx(), reqWith(t, "miniapp.calendar.create", map[string]any{
		"summary": "구글 미러", "start": "2026-06-10T14:00:00+09:00",
	}))
	var ev calendarEventOut
	decode(t, resp, &ev)
	if len(w.pushed) != 1 || w.pushed[0] != ev.ID {
		t.Errorf("create should mirror the new event id, pushed=%v id=%s", w.pushed, ev.ID)
	}
}

func TestCalendarCreate_SucceedsEvenWhenMirrorFails(t *testing.T) {
	w := &fakeWriter{pushErr: true}
	deps, store := depsWithWriter(t, w)
	resp := calendarCreate(deps)(authedCtx(), reqWith(t, "miniapp.calendar.create", map[string]any{
		"summary": "미러 실패해도 저장", "start": "2026-06-10T14:00:00+09:00",
	}))
	if !resp.OK {
		t.Fatal("create must succeed even when the Google mirror fails (local is source of truth)")
	}
	got := store.ListRange(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 {
		t.Fatalf("event must persist locally, store has %d", len(got))
	}
}

func TestCalendarDelete_MirrorsRemoval(t *testing.T) {
	w := &fakeWriter{}
	deps, store := depsWithWriter(t, w)
	created, err := store.Create(localcal.CreateInput{Summary: "삭제 대상", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	resp := calendarDelete(deps)(authedCtx(), reqWith(t, "miniapp.calendar.delete", map[string]string{"id": created.ID}))
	if !resp.OK {
		t.Fatalf("delete failed: %+v", resp)
	}
	if len(w.removed) != 1 || w.removed[0] != created.ID {
		t.Errorf("delete should mirror removal, removed=%v", w.removed)
	}
}

func TestCalendarListMerged_DropsMirroredGoogleCopies(t *testing.T) {
	// Google returns two events; one is a Deneb-authored mirror (id gDUP) that the
	// local store also provides. The merge must show it once, not twice.
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, _, _ time.Time, _ int) ([]calendar.Event, error) {
			return []calendar.Event{
				{ID: "gDUP", Summary: "미러된 로컬 일정", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC)},
				{ID: "gREAL", Summary: "구글 전용 일정", Start: time.Date(2026, 6, 10, 6, 0, 0, 0, time.UTC)},
			}, nil
		},
	}
	deps, store := calDepsWithLocal(t, client)
	if _, err := store.Create(localcal.CreateInput{Summary: "미러된 로컬 일정", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	deps.Writer = func() (CalendarWriter, error) {
		return &fakeWriter{mirrored: map[string]struct{}{"gDUP": {}}}, nil
	}

	resp := calendarListRange(deps)(authedCtx(), reqWith(t, "miniapp.calendar.list_range", map[string]string{
		"from": "2026-06-01T00:00:00Z", "to": "2026-06-30T00:00:00Z",
	}))
	var out struct {
		Events []calendarEventOut `json:"events"`
	}
	decode(t, resp, &out)
	// gREAL (google-only) + the local copy of the mirrored event = 2; gDUP dropped.
	var sawGDUP bool
	for _, e := range out.Events {
		if e.ID == "gDUP" {
			sawGDUP = true
		}
	}
	if sawGDUP {
		t.Errorf("mirrored Google copy gDUP should be dropped, events=%+v", out.Events)
	}
	if len(out.Events) != 2 {
		t.Errorf("want 2 events (gREAL + local), got %d: %+v", len(out.Events), out.Events)
	}
}

func TestCalendarCreate_NoWriterIsFine(t *testing.T) {
	deps, _ := calDepsWithLocal(t, nil) // Writer nil
	resp := calendarCreate(deps)(authedCtx(), reqWith(t, "miniapp.calendar.create", map[string]any{
		"summary": "미러 없음", "start": "2026-06-10T14:00:00+09:00",
	}))
	if !resp.OK {
		t.Fatalf("create must work without a Writer: %+v", resp)
	}
}
