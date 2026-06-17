package mailarchive

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestPrioritizedArchiveUIDGroupsPreservesThreadAncestors(t *testing.T) {
	got := prioritizedArchiveUIDGroups(
		[]string{"3", "1", "2", "2"},
		[]string{"1", "10", "11", "12", "13", "14", "15", "16"},
		2,
		3,
		4,
	)
	want := [][]string{{"3", "1"}, {"15", "16"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPrioritizedArchiveUIDGroupsCapsSenderRecentOnly(t *testing.T) {
	got := prioritizedArchiveUIDGroups(nil, []string{"1", "2", "3", "4"}, 2, 2, 10)
	want := [][]string{{"3", "4"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPrioritizedArchiveUIDGroupsDisablesZeroCaps(t *testing.T) {
	got := prioritizedArchiveUIDGroups([]string{"1", "2"}, []string{"3", "4"}, 0, 0, 10)
	if got != nil {
		t.Fatalf("got %v want nil", got)
	}
}

func TestNewSourceCapsReferenceSearches(t *testing.T) {
	src := New(Config{Addr: "127.0.0.1:1143"})
	if src == nil {
		t.Fatal("source should be configured with an address")
	}
	if src.maxReferences != defaultMaxReferences {
		t.Fatalf("maxReferences = %d, want %d", src.maxReferences, defaultMaxReferences)
	}
}

func TestSameArchivedMessageByMessageID(t *testing.T) {
	current := &gmail.MessageDetail{MessageIDHeader: "<A@Example.COM>"}
	archived := &gmail.MessageDetail{MessageIDHeader: " <a@example.com> "}
	if !sameArchivedMessage(current, archived, normalizeMsgID(current.MessageIDHeader)) {
		t.Fatal("expected matching Message-ID to identify the same message")
	}

	archived.MessageIDHeader = ""
	if sameArchivedMessage(current, archived, normalizeMsgID(current.MessageIDHeader)) {
		t.Fatal("did not expect missing archived Message-ID to match current Message-ID")
	}
}

func TestSameArchivedMessageFallbackWithoutMessageID(t *testing.T) {
	current := &gmail.MessageDetail{
		From:    "Sender Name <sender@example.com>",
		Subject: "Quarterly Update",
		Date:    "Tue, 16 Jun 2026 12:34:56 +0900",
		Body:    "Line one\n\nLine two",
	}
	archived := &gmail.MessageDetail{
		From:    " sender name   <sender@example.com> ",
		Subject: "Quarterly   Update",
		Date:    "Tue, 16 Jun 2026 12:34:56 +0900",
		Body:    "Line one Line two",
	}
	if !sameArchivedMessage(current, archived, "") {
		t.Fatal("expected fallback match without Message-ID")
	}

	archived.Date = "Tue, 16 Jun 2026 12:35:56 +0900"
	if sameArchivedMessage(current, archived, "") {
		t.Fatal("did not expect fallback to match with a different Date")
	}
}

func TestArchivedMessageDedupeKeyFallsBackWithoutMessageID(t *testing.T) {
	first := &gmail.MessageDetail{
		From:    "Sender <sender@example.com>",
		Subject: "Same subject",
		Date:    "Tue, 16 Jun 2026 12:34:56 +0900",
		Body:    "Body line one\nBody line two",
	}
	second := &gmail.MessageDetail{
		From:    " sender <sender@example.com> ",
		Subject: "Same   subject",
		Date:    "Tue, 16 Jun 2026 12:34:56 +0900",
		Body:    "Body line one Body line two",
	}
	firstKey := archivedMessageDedupeKey(first)
	secondKey := archivedMessageDedupeKey(second)
	if firstKey == "" {
		t.Fatal("expected fallback dedupe key")
	}
	if firstKey != secondKey {
		t.Fatalf("got keys %q and %q, want equal", firstKey, secondKey)
	}

	second.Body = "Different body"
	if archivedMessageDedupeKey(second) == firstKey {
		t.Fatal("did not expect different body to dedupe")
	}
}
