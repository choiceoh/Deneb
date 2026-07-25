package schedule

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// fakeCalWriter records what the chat tool asked the Google mirror to do.
type fakeCalWriter struct {
	pushed  []tooldeps.CalendarEvent
	removed []string
	err     error
}

func (f *fakeCalWriter) Push(_ context.Context, localID string, ev tooldeps.CalendarEvent) error {
	ev.ID = localID
	f.pushed = append(f.pushed, ev)
	return f.err
}

func (f *fakeCalWriter) Remove(_ context.Context, localID string) error {
	f.removed = append(f.removed, localID)
	return f.err
}

func depsWithWriter(t *testing.T, w tooldeps.CalendarWriter) *tooldeps.CalendarDeps {
	t.Helper()
	d := depsWith(&fakeCalReader{}, wrapTestLocalCal(newTestLocalCal(t)))
	d.Writer = func() (tooldeps.CalendarWriter, error) { return w, nil }
	return d
}

func createEvent(t *testing.T, d *tooldeps.CalendarDeps) string {
	t.Helper()
	return callCal(t, d, map[string]any{
		"action": "create", "summary": "탑솔라 미팅", "start": "2026-08-03T15:00:00+09:00",
	})
}

// Chat is this agent's PRIMARY calendar surface, but #4209/#4210 wired the
// Google mirror only into the miniapp RPC: an event created in the app UI
// reached Google and the identical event created by saying "내일 3시 미팅
// 잡아줘" silently did not.
func TestCalendarToolMirrorsCreateUpdateDeleteToGoogle(t *testing.T) {
	w := &fakeCalWriter{}
	d := depsWithWriter(t, w)

	created := createEvent(t, d)
	id := extractCalID(created)
	if id == "" {
		t.Fatalf("no id in create output: %s", created)
	}
	if len(w.pushed) != 1 || w.pushed[0].ID != id || w.pushed[0].Summary != "탑솔라 미팅" {
		t.Fatalf("create must mirror once, keyed by the local id: %+v", w.pushed)
	}

	callCal(t, d, map[string]any{
		"action": "update", "id": id, "summary": "탑솔라 미팅(수정)", "start": "2026-08-03T16:00:00+09:00",
	})
	if len(w.pushed) != 2 || w.pushed[1].Summary != "탑솔라 미팅(수정)" {
		t.Fatalf("update must mirror the edited event: %+v", w.pushed)
	}
	// The same local id is what makes the syncer PATCH the existing Google event
	// instead of inserting a duplicate.
	if w.pushed[1].ID != id {
		t.Errorf("update must reuse local id %s, got %s", id, w.pushed[1].ID)
	}

	callCal(t, d, map[string]any{"action": "delete", "id": id})
	if len(w.removed) != 1 || w.removed[0] != id {
		t.Fatalf("delete must remove the mirror: %+v", w.removed)
	}
}

// The local store is the source of truth: a Google outage must never turn a
// successful local write into a user-visible failure, and an unwired/failing
// writer is the ordinary local-only degrade. Same contract the miniapp handler
// encodes for its own mirror.
func TestCalendarToolMirrorFailureLeavesLocalWriteSuccessful(t *testing.T) {
	cases := map[string]*tooldeps.CalendarDeps{
		"push error":       depsWithWriter(t, &fakeCalWriter{err: errors.New("google 503")}),
		"nil writer":       depsWith(&fakeCalReader{}, wrapTestLocalCal(newTestLocalCal(t))),
		"factory error":    depsWith(&fakeCalReader{}, wrapTestLocalCal(newTestLocalCal(t))),
		"nil from factory": depsWith(&fakeCalReader{}, wrapTestLocalCal(newTestLocalCal(t))),
	}
	cases["factory error"].Writer = func() (tooldeps.CalendarWriter, error) {
		return nil, errors.New("no credentials")
	}
	cases["nil from factory"].Writer = func() (tooldeps.CalendarWriter, error) { return nil, nil }

	for name, d := range cases {
		if out := createEvent(t, d); !strings.Contains(out, "일정을 추가했습니다") {
			t.Errorf("%s: local create must still succeed, got %s", name, out)
		}
	}
}
