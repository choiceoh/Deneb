package mailwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNilAndEmptyStoreContracts(t *testing.T) {
	var nilStore *Store
	if got := nilStore.Get("id"); got != (MessageState{}) {
		t.Fatalf("nil Get = %+v", got)
	}
	if got := nilStore.Summary(); got != (Summary{}) {
		t.Fatalf("nil Summary = %+v", got)
	}
	if got, err := nilStore.SummaryWithError(); err != nil || got != (Summary{}) {
		t.Fatalf("nil SummaryWithError = %+v/%v", got, err)
	}
	for _, call := range []func() (MessageState, error){
		func() (MessageState, error) { return nilStore.RememberMessage(MessageInput{ID: "id"}) },
		func() (MessageState, error) { return nilStore.MarkAnalysisAnalyzing(MessageInput{ID: "id"}) },
		func() (MessageState, error) { return nilStore.MarkAnalysisReview(MessageInput{ID: "id"}, "unknown") },
		func() (MessageState, error) {
			return nilStore.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "id"}})
		},
		func() (MessageState, error) {
			return nilStore.MarkAnalysisFailed(MessageInput{ID: "id"}, errors.New("x"))
		},
		func() (MessageState, error) { return nilStore.MarkFeedCreated("id") },
		func() (MessageState, error) { return nilStore.MarkDerivedCounts("id", 1, 1) },
	} {
		if got, err := call(); err != nil || got != (MessageState{}) {
			t.Errorf("nil call = %+v/%v", got, err)
		}
	}
	empty := New("")
	if got, err := empty.RememberMessage(MessageInput{ID: "id", Subject: "subject"}); err != nil || got.ID != "id" || got.Subject != "subject" {
		t.Fatalf("memory-only update = %+v/%v", got, err)
	}
	// Empty-path stores intentionally have no persistence between operations.
	if got := empty.Get("id"); got.ID != "" {
		t.Fatalf("empty path persisted unexpectedly: %+v", got)
	}
}

func TestMessageWorkflow_TransitionsThroughStatesAndReloadsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mailwork.json")
	s := New(path)
	in := MessageInput{ID: "  msg-1  ", ThreadID: " thread ", From: " sender ", Subject: " subject ", Date: " date ", Mailbox: " INBOX ", HasAttachment: true, AttachmentCount: 2}
	remembered, err := s.RememberMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if remembered.ID != "msg-1" || remembered.ThreadID != "thread" || remembered.From != "sender" || remembered.Subject != "subject" || remembered.Mailbox != "INBOX" || !remembered.HasAttachment || remembered.AttachmentCount != 2 || remembered.CreatedAtMs == 0 || remembered.LastSeenAtMs == 0 {
		t.Fatalf("remembered = %+v", remembered)
	}
	analyzing, err := s.MarkAnalysisAnalyzing(MessageInput{ID: "msg-1", Subject: " updated "})
	if err != nil || analyzing.AnalysisStatus != AnalysisAnalyzing || analyzing.Subject != "updated" || analyzing.AnalysisUpdatedAtMs == 0 {
		t.Fatalf("analyzing = %+v/%v", analyzing, err)
	}
	done, err := s.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "msg-1"}, Quality: " high ", DerivedCountsKnown: true, CalendarProposalCount: 2, TodoCount: 3, DurationMs: -1})
	if err != nil || done.AnalysisStatus != AnalysisDone || done.AnalysisQuality != "high" || done.CalendarProposalCount != 2 || done.TodoCount != 3 || done.AnalysisDurationMs != 0 || done.LastError != "" {
		t.Fatalf("done = %+v/%v", done, err)
	}
	failed, err := s.MarkAnalysisFailed(MessageInput{ID: "msg-1"}, errors.New("analysis failed"))
	if err != nil || failed.AnalysisStatus != AnalysisFailed || failed.LastError != "analysis failed" {
		t.Fatalf("failed = %+v/%v", failed, err)
	}
	feed, err := s.MarkFeedCreated("msg-1")
	if err != nil || feed.FeedStatus != FeedCreated || feed.LastError != "" || feed.FeedUpdatedAtMs == 0 {
		t.Fatalf("feed = %+v/%v", feed, err)
	}
	counts, err := s.MarkDerivedCounts("msg-1", -2, 4)
	if err != nil || counts.CalendarProposalCount != 0 || counts.TodoCount != 4 {
		t.Fatalf("counts = %+v/%v", counts, err)
	}
	reloaded := New(path).Get("msg-1")
	if !reflect.DeepEqual(reloaded, counts) {
		t.Fatalf("reloaded = %+v, want %+v", reloaded, counts)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v/%v", info, err)
	}
}

func TestDerivedCountsUnknownPreservesExisting(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"))
	if _, err := s.MarkDerivedCounts("id", 5, 6); err != nil {
		t.Fatal(err)
	}
	got, err := s.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "id"}, DerivedCountsKnown: false})
	if err != nil || got.CalendarProposalCount != 5 || got.TodoCount != 6 {
		t.Fatalf("unknown = %+v/%v", got, err)
	}
	got, err = s.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "id"}, DerivedCountsKnown: true, CalendarProposalCount: -1, TodoCount: -2})
	if err != nil || got.CalendarProposalCount != 0 || got.TodoCount != 0 {
		t.Fatalf("known negative = %+v/%v", got, err)
	}
}

func TestMergeMessageInputMonotonicAttachmentContract(t *testing.T) {
	ms := MessageState{ThreadID: "old-thread", From: "old-from", Subject: "old-subject", Date: "old-date", Mailbox: "old-box", HasAttachment: true, AttachmentCount: 5}
	mergeMessageInput(&ms, MessageInput{ThreadID: " ", From: "", Subject: " new ", AttachmentCount: 2})
	if ms.ThreadID != "old-thread" || ms.From != "old-from" || ms.Subject != "new" || !ms.HasAttachment || ms.AttachmentCount != 5 {
		t.Fatalf("merge = %+v", ms)
	}
	mergeMessageInput(&ms, MessageInput{ThreadID: "next", HasAttachment: false, AttachmentCount: 7})
	if ms.ThreadID != "next" || !ms.HasAttachment || ms.AttachmentCount != 7 {
		t.Fatalf("second merge = %+v", ms)
	}
	mergeMessageInput(nil, MessageInput{Subject: "ignored"})
}

func TestSummarizeAllStatesContract(t *testing.T) {
	msgs := map[string]MessageState{
		"done-feed":    {AnalysisStatus: AnalysisDone, FeedStatus: FeedCreated, CalendarProposalCount: 2, TodoCount: 1, UpdatedAtMs: 10},
		"done-missing": {AnalysisStatus: AnalysisDone, CalendarProposalCount: 1, UpdatedAtMs: 20},
		"analyzing":    {AnalysisStatus: AnalysisAnalyzing, TodoCount: 2, UpdatedAtMs: 15},
		"queued":       {AnalysisStatus: AnalysisQueued, UpdatedAtMs: 5},
		"failed":       {AnalysisStatus: AnalysisFailed, FeedStatus: FeedFailed, UpdatedAtMs: 8},
		"review":       {AnalysisStatus: AnalysisReview, ReviewReason: "unknown", UpdatedAtMs: 9},
		"stale":        {AnalysisStatus: AnalysisStale, CalendarProposalCount: -1, TodoCount: -1, UpdatedAtMs: 2},
	}
	got := summarize(msgs)
	want := Summary{Messages: 7, Analyzed: 2, Analyzing: 2, Failed: 1, FeedCreated: 1, FeedMissing: 1, CalendarCandidates: 3, TodoCandidates: 3, UpdatedAtMs: 20}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
	if got := summarize(nil); got != (Summary{}) {
		t.Fatalf("nil summary = %+v", got)
	}
}

func TestReviewStateTransitionsAndHintContract(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"))
	reviewed, err := s.MarkAnalysisReview(MessageInput{ID: "mail", From: "new@example.test"}, "  unknown sender  ")
	if err != nil || reviewed.AnalysisStatus != AnalysisReview || reviewed.ReviewReason != "unknown sender" || reviewed.Hint() != "unknown sender" || reviewed.LastError != "" {
		t.Fatalf("reviewed = %+v/%v", reviewed, err)
	}
	analyzing, err := s.MarkAnalysisAnalyzing(MessageInput{ID: "mail"})
	if err != nil || analyzing.AnalysisStatus != AnalysisAnalyzing || analyzing.ReviewReason != "" || analyzing.Hint() != "" {
		t.Fatalf("analyzing = %+v/%v", analyzing, err)
	}
	failed, err := s.MarkAnalysisFailed(MessageInput{ID: "mail"}, errors.New("provider down"))
	if err != nil || failed.Hint() != "provider down" {
		t.Fatalf("failed = %+v/%v", failed, err)
	}
}

func TestErrorAndNumericHelpers(t *testing.T) {
	if errorText(nil) != "" || errorText(context.Canceled) != "context canceled" || errorText(fmt.Errorf("wrapped: %w", context.Canceled)) != "context canceled" || errorText(context.DeadlineExceeded) != "context deadline exceeded" {
		t.Fatal("errorText contract")
	}
	long := strings.Repeat("가", maxLastErrorChars+2)
	got := truncateError("  " + long + "  ")
	if utf8.RuneCountInString(got) != maxLastErrorChars || !utf8.ValidString(got) {
		t.Fatalf("truncate len=%d valid=%v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if truncateError(" short ") != "short" || truncateError("") != "" {
		t.Fatal("truncate short")
	}
	for _, tt := range []struct{ a, b, want int64 }{{1, 2, 2}, {2, 1, 2}, {-1, -2, -1}, {0, 0, 0}} {
		if maxInt64(tt.a, tt.b) != tt.want {
			t.Errorf("max(%d,%d)", tt.a, tt.b)
		}
	}
	for _, tt := range []struct{ in, want int }{{-2, 0}, {-1, 0}, {0, 0}, {2, 2}} {
		if nonNegativeInt(tt.in) != tt.want {
			t.Errorf("nonNegative(%d)", tt.in)
		}
	}
}

func TestCorruptAndUnreadableStorageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if got := s.Get("id"); got != (MessageState{}) {
		t.Fatalf("corrupt Get = %+v", got)
	}
	if got := s.Summary(); got != (Summary{}) {
		t.Fatalf("corrupt Summary = %+v", got)
	}
	if _, err := s.SummaryWithError(); err == nil {
		t.Fatal("corrupt SummaryWithError swallowed")
	}
	if _, err := s.RememberMessage(MessageInput{ID: "id"}); err == nil {
		t.Fatal("corrupt update swallowed")
	}
	parentFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := New(filepath.Join(parentFile, "state.json"))
	if _, err := blocked.RememberMessage(MessageInput{ID: "id"}); err == nil {
		t.Fatal("blocked save swallowed")
	}
}

func TestLoadNullAndEmptyModelContracts(t *testing.T) {
	for _, body := range []string{"{}", `{"messages":null}`, "null"} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := New(path).loadLocked()
		if err != nil || got.Messages == nil || len(got.Messages) != 0 {
			t.Errorf("body %s = %+v/%v", body, got, err)
		}
	}
}

func TestStoreLockKeyContract(t *testing.T) {
	if got := storeLockKey(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := storeLockKey(" \n"); got != "" {
		t.Fatalf("blank = %q", got)
	}
	rel := filepath.Join("relative", "..", "state.json")
	got := storeLockKey(rel)
	if !filepath.IsAbs(got) || filepath.Base(got) != "state.json" || strings.Contains(got, "..") {
		t.Fatalf("key = %q", got)
	}
}

func TestConcurrentStoresSamePathDoNotLoseUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	stores := []*Store{New(path), New(path), New(path), New(path)}
	const count = 48
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := stores[i%len(stores)].RememberMessage(MessageInput{ID: time.Unix(int64(i), 0).UTC().Format("150405"), Subject: "message"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("update error: %v", err)
		}
	}
	got, err := stores[0].SummaryWithError()
	if err != nil || got.Messages != count {
		t.Fatalf("summary = %+v/%v", got, err)
	}
}
