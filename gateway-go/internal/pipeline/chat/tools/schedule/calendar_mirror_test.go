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

// The Google mirror has no retry, no backlog and no reconciliation pass, so a
// failed push is permanent: the event lives locally and never reaches the
// calendar the operator reads on their phone. Discarding the error left the
// result claiming a clean 추가 while only a Warn line knew otherwise.
func TestCalendarToolReportsGoogleMirrorFailure(t *testing.T) {
	w := &fakeCalWriter{err: errors.New("insufficient authentication scopes")}
	d := depsWithWriter(t, w)

	created := createEvent(t, d)
	if !strings.Contains(created, "일정을 추가했습니다") {
		t.Fatalf("the local write must still succeed:\n%s", created)
	}
	if !strings.Contains(created, "⚠️") || !strings.Contains(created, "구글 캘린더") {
		t.Errorf("a failed mirror was reported as a clean create:\n%s", created)
	}
	if !strings.Contains(created, "insufficient authentication scopes") {
		t.Errorf("notice dropped the cause the operator needs:\n%s", created)
	}
	if !strings.Contains(created, "재시도") {
		t.Errorf("notice did not say the divergence is permanent:\n%s", created)
	}

	id := extractCalID(created)
	updated := callCal(t, d, map[string]any{
		"action": "update", "id": id, "summary": "탑솔라 미팅 (수정)",
		"start": "2026-08-03T16:00:00+09:00",
	})
	if !strings.Contains(updated, "⚠️") || !strings.Contains(updated, "구글 캘린더") {
		t.Errorf("update did not report the mirror failure:\n%s", updated)
	}
	deleted := callCal(t, d, map[string]any{"action": "delete", "id": id})
	if !strings.Contains(deleted, "⚠️") || !strings.Contains(deleted, "구글 캘린더") {
		t.Errorf("delete did not report the mirror failure:\n%s", deleted)
	}
}

// A healthy mirror stays quiet, and so does a deliberately local-only setup —
// otherwise the warning fires constantly and stops meaning anything.
func TestCalendarToolStaysQuietWhenTheMirrorIsHealthyOrOff(t *testing.T) {
	healthy := createEvent(t, depsWithWriter(t, &fakeCalWriter{}))
	if strings.Contains(healthy, "구글 캘린더") {
		t.Errorf("a successful mirror warned:\n%s", healthy)
	}

	localOnly := depsWith(&fakeCalReader{}, wrapTestLocalCal(newTestLocalCal(t)))
	localOnly.Writer = func() (tooldeps.CalendarWriter, error) {
		return nil, errors.New("Google Calendar 쓰기 동기화가 꺼져 있습니다")
	}
	off := createEvent(t, localOnly)
	if strings.Contains(off, "구글 캘린더") {
		t.Errorf("a deliberately local-only setup warned:\n%s", off)
	}
}
