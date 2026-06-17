package mailwork

import (
	"errors"
	"path/filepath"
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
	_, _ = store.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "done"}, CalendarProposalCount: 1})
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
