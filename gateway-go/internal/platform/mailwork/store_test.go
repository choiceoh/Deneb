package mailwork

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRememberAndMarkAnalysisDone(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))

	if _, err := store.RememberMessage(MessageInput{
		ID:              "m1",
		ThreadID:        "t1",
		From:            "sender@example.com",
		Subject:         "견적 요청",
		Date:            "2026-06-17T01:00:00Z",
		Mailbox:         "INBOX",
		HasAttachment:   true,
		AttachmentCount: 1,
	}); err != nil {
		t.Fatalf("RememberMessage: %v", err)
	}
	if _, err := store.MarkAnalysisDone(AnalysisInput{
		MessageInput:          MessageInput{ID: "m1", Subject: "견적 요청"},
		Quality:               "attention",
		DerivedCountsKnown:    true,
		CalendarProposalCount: 1,
		TodoCount:             2,
		DurationMs:            321,
	}); err != nil {
		t.Fatalf("MarkAnalysisDone: %v", err)
	}

	got := store.Get("m1")
	if got.AnalysisStatus != AnalysisDone || got.AnalysisQuality != "attention" {
		t.Fatalf("analysis = %q/%q", got.AnalysisStatus, got.AnalysisQuality)
	}
	if got.ThreadID != "t1" || !got.HasAttachment || got.AttachmentCount != 1 {
		t.Fatalf("metadata was not preserved: %+v", got)
	}
	if got.CalendarProposalCount != 1 || got.TodoCount != 2 {
		t.Fatalf("derived counts = %d/%d", got.CalendarProposalCount, got.TodoCount)
	}

	reopened := New(filepath.Join(filepath.Dir(store.path), "mail_work_state.json"))
	if reopened.Get("m1").AnalysisStatus != AnalysisDone {
		t.Fatalf("state did not persist: %+v", reopened.Get("m1"))
	}
}

func TestStoreSummary(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))
	_, _ = store.MarkAnalysisDone(AnalysisInput{
		MessageInput:          MessageInput{ID: "done"},
		DerivedCountsKnown:    true,
		CalendarProposalCount: 1,
	})
	_, _ = store.MarkAnalysisAnalyzing(MessageInput{ID: "running"})
	_, _ = store.MarkAnalysisFailed(MessageInput{ID: "failed"}, errors.New("provider down"))
	_, _ = store.MarkFeedCreated("done")
	_, _ = store.MarkDerivedCounts("todo", 0, 2)

	got := store.Summary()
	if got.Messages != 4 || got.Analyzed != 1 || got.Analyzing != 1 || got.Failed != 1 {
		t.Fatalf("summary counts = %+v", got)
	}
	if got.FeedCreated != 1 || got.FeedMissing != 0 {
		t.Fatalf("feed counts = %+v", got)
	}
	if got.CalendarCandidates != 1 || got.TodoCandidates != 2 {
		t.Fatalf("derived summary = %+v", got)
	}
}

func TestStoreMarkAnalysisDoneReplacesKnownDerivedCounts(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))

	if _, err := store.MarkAnalysisDone(AnalysisInput{
		MessageInput:          MessageInput{ID: "m1"},
		DerivedCountsKnown:    true,
		CalendarProposalCount: 2,
		TodoCount:             1,
	}); err != nil {
		t.Fatalf("seed MarkAnalysisDone: %v", err)
	}
	if _, err := store.MarkAnalysisDone(AnalysisInput{
		MessageInput:       MessageInput{ID: "m1"},
		DerivedCountsKnown: true,
	}); err != nil {
		t.Fatalf("replacement MarkAnalysisDone: %v", err)
	}

	got := store.Get("m1")
	if got.CalendarProposalCount != 0 || got.TodoCount != 0 {
		t.Fatalf("known zero counts should replace stale derived counts: %+v", got)
	}
}

func TestStoreMarkAnalysisDonePreservesUnknownDerivedCounts(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))

	if _, err := store.MarkAnalysisDone(AnalysisInput{
		MessageInput:          MessageInput{ID: "m1"},
		DerivedCountsKnown:    true,
		CalendarProposalCount: 1,
		TodoCount:             2,
	}); err != nil {
		t.Fatalf("seed MarkAnalysisDone: %v", err)
	}
	if _, err := store.MarkAnalysisDone(AnalysisInput{
		MessageInput: MessageInput{ID: "m1"},
		Quality:      "routine",
	}); err != nil {
		t.Fatalf("cache-hydration MarkAnalysisDone: %v", err)
	}

	got := store.Get("m1")
	if got.CalendarProposalCount != 1 || got.TodoCount != 2 {
		t.Fatalf("unknown counts should preserve existing downstream state: %+v", got)
	}
}

func TestStoreMarkDerivedCountsReplacesCounts(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))

	if _, err := store.MarkDerivedCounts("m1", 3, 2); err != nil {
		t.Fatalf("seed MarkDerivedCounts: %v", err)
	}
	if _, err := store.MarkDerivedCounts("m1", 0, 0); err != nil {
		t.Fatalf("replacement MarkDerivedCounts: %v", err)
	}

	got := store.Get("m1")
	if got.CalendarProposalCount != 0 || got.TodoCount != 0 {
		t.Fatalf("derived counts should be exact latest values: %+v", got)
	}
}

func TestStoreMarkAnalysisFailedTruncatesError(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mail_work_state.json"))
	long := ""
	for i := 0; i < maxLastErrorChars+20; i++ {
		long += "가"
	}
	if _, err := store.MarkAnalysisFailed(MessageInput{ID: "m1"}, errors.New(long)); err != nil {
		t.Fatalf("MarkAnalysisFailed: %v", err)
	}
	if got := []rune(store.Get("m1").LastError); len(got) != maxLastErrorChars {
		t.Fatalf("last error rune len = %d, want %d", len(got), maxLastErrorChars)
	}
}

func TestStoreConcurrentInstancesPreserveUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail_work_state.json")
	for i := 0; i < 50; i++ {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove state: %v", err)
		}
		analysisStore := New(path)
		feedStore := New(path)

		var wg sync.WaitGroup
		errs := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := analysisStore.MarkAnalysisDone(AnalysisInput{
				MessageInput:       MessageInput{ID: "m1", Subject: "견적 요청"},
				Quality:            "attention",
				DerivedCountsKnown: true,
				TodoCount:          1,
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := feedStore.MarkFeedCreated("m1")
			errs <- err
		}()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent update: %v", err)
			}
		}

		got := New(path).Get("m1")
		if got.AnalysisStatus != AnalysisDone || got.FeedStatus != FeedCreated || got.TodoCount != 1 {
			t.Fatalf("iteration %d lost a concurrent update: %+v", i, got)
		}
	}
}

func TestStoreDoesNotOverwriteCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail_work_state.json")
	original := []byte("{not-json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}

	if _, err := New(path).MarkFeedCreated("m1"); err == nil {
		t.Fatalf("expected corrupt state error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("corrupt state was overwritten: %q", string(got))
	}
}

func TestStoreSummaryWithErrorSurfacesCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail_work_state.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}

	summary, err := New(path).SummaryWithError()
	if err == nil {
		t.Fatalf("expected corrupt state error")
	}
	if summary.Messages != 0 {
		t.Fatalf("summary = %+v, want zero on load error", summary)
	}
}
