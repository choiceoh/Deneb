package dropboxpoll

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
)

type fakeClient struct {
	latestCursor string
	changes      []dropbox.Entry
	newCursor    string
	listErr      error
	listCalls    int
}

func (f *fakeClient) LatestCursor(_ context.Context, _ string, _ bool) (string, error) {
	return f.latestCursor, nil
}

func (f *fakeClient) ListChanges(_ context.Context, _ string) ([]dropbox.Entry, string, error) {
	f.listCalls++
	return f.changes, f.newCursor, f.listErr
}

type fakeAgent struct {
	prompts []string
	reply   string
}

func (a *fakeAgent) RunAgentTurn(_ context.Context, prompt string) (string, error) {
	a.prompts = append(a.prompts, prompt)
	return a.reply, nil
}

type fakeNotifier struct {
	msgs []string
	err  error
}

func (n *fakeNotifier) Notify(_ context.Context, msg string) error {
	n.msgs = append(n.msgs, msg)
	return n.err
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(Config{StateDir: t.TempDir(), FolderPath: "/Deneb-Inbox"}, slog.Default())
}

// First run snapshots the cursor "from now" and does NOT analyze the backlog.
func TestPoll_FirstRunSnapshotsCursor(t *testing.T) {
	svc := newTestService(t)
	agent := &fakeAgent{}
	svc.SetAgent(agent)
	fc := &fakeClient{latestCursor: "cur-init"}

	if err := svc.poll(context.Background(), fc); err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 0 {
		t.Error("first run must not trigger analysis")
	}
	if fc.listCalls != 0 {
		t.Error("first run must not call ListChanges")
	}
	st, _ := svc.state.Load()
	if st.Cursor != "cur-init" {
		t.Errorf("cursor = %q, want cur-init", st.Cursor)
	}
}

// New files trigger an agent turn and deliver its summary via the notifier.
func TestPoll_NewFilesTriggerAgent(t *testing.T) {
	svc := newTestService(t)
	if err := svc.state.Save(&PollState{Cursor: "cur0"}); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{reply: "신규 계약서 1건 분석 완료"}
	notifier := &fakeNotifier{}
	svc.SetAgent(agent)
	svc.SetNotifier(notifier)
	fc := &fakeClient{
		changes:   []dropbox.Entry{{ID: "id1", Tag: "file", Name: "a.pdf", PathDisplay: "/Deneb-Inbox/a.pdf"}},
		newCursor: "cur1",
	}

	if err := svc.poll(context.Background(), fc); err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agent.prompts))
	}
	if !strings.Contains(agent.prompts[0], "a.pdf") {
		t.Errorf("prompt missing filename: %q", agent.prompts[0])
	}
	if len(notifier.msgs) != 1 || notifier.msgs[0] != "신규 계약서 1건 분석 완료" {
		t.Errorf("notifier msgs = %+v", notifier.msgs)
	}
	st, _ := svc.state.Load()
	if st.Cursor != "cur1" {
		t.Errorf("cursor not advanced: %q", st.Cursor)
	}
	if !st.hasSeen("id1") {
		t.Error("processed file not marked seen")
	}
}

// Already-seen files are skipped without re-analysis.
func TestPoll_SeenFilesSkipped(t *testing.T) {
	svc := newTestService(t)
	if err := svc.state.Save(&PollState{Cursor: "cur0", SeenIDs: []string{"id1"}}); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{}
	svc.SetAgent(agent)
	fc := &fakeClient{
		changes:   []dropbox.Entry{{ID: "id1", Tag: "file", Name: "a.pdf"}},
		newCursor: "cur1",
	}

	if err := svc.poll(context.Background(), fc); err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 0 {
		t.Error("seen file should not be re-analyzed")
	}
	st, _ := svc.state.Load()
	if st.Cursor != "cur1" {
		t.Errorf("cursor should still advance on no-fresh: %q", st.Cursor)
	}
}

// Notify failure holds state (cursor/seen unchanged) so the report is retried
// next cycle instead of being silently lost.
func TestPoll_NotifyFailureHoldsState(t *testing.T) {
	svc := newTestService(t)
	if err := svc.state.Save(&PollState{Cursor: "cur0"}); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{reply: "분석 요약"}
	notifier := &fakeNotifier{err: errors.New("push down")}
	svc.SetAgent(agent)
	svc.SetNotifier(notifier)
	fc := &fakeClient{
		changes:   []dropbox.Entry{{ID: "id1", Tag: "file", Name: "a.pdf"}},
		newCursor: "cur1",
	}

	if err := svc.poll(context.Background(), fc); err == nil {
		t.Fatal("expected poll to return the notify error")
	}
	st, _ := svc.state.Load()
	if st.Cursor == "cur1" {
		t.Error("cursor advanced despite notify failure")
	}
	if st.hasSeen("id1") {
		t.Error("file marked seen despite notify failure")
	}
}

// An expired cursor (ErrCursorReset) re-seeds from LatestCursor instead of
// wedging, and skips analysis that cycle.
func TestPoll_CursorReset(t *testing.T) {
	svc := newTestService(t)
	if err := svc.state.Save(&PollState{Cursor: "stale"}); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{}
	svc.SetAgent(agent)
	fc := &fakeClient{listErr: dropbox.ErrCursorReset, latestCursor: "fresh"}

	if err := svc.poll(context.Background(), fc); err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 0 {
		t.Error("reset cycle must not analyze")
	}
	st, _ := svc.state.Load()
	if st.Cursor != "fresh" {
		t.Errorf("cursor not re-seeded: %q", st.Cursor)
	}
}
