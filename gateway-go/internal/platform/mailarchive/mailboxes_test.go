package mailarchive

import (
	"reflect"
	"testing"
)

func TestSelectMailboxesUsesConfiguredArchiveNames(t *testing.T) {
	configured := []string{"INBOX", "Archive", "Archive", " "}

	if got := SelectMailboxes("", configured); !reflect.DeepEqual(got, []string{"INBOX", "Archive"}) {
		t.Fatalf("all selector = %#v, want configured mailboxes", got)
	}
	if got := SelectMailboxes("archive", configured); !reflect.DeepEqual(got, []string{"Archive"}) {
		t.Fatalf("archive selector = %#v, want non-INBOX configured mailbox", got)
	}
	if got := SelectMailboxes("legacy_gmail", configured); !reflect.DeepEqual(got, []string{"Gmail"}) {
		t.Fatalf("legacy_gmail selector = %#v, want legacy Gmail mailbox", got)
	}
}

func TestLookupMailboxCandidatesKeepsLegacyGmailLocatorReadable(t *testing.T) {
	got := lookupMailboxCandidates("Gmail")
	want := []string{"Gmail", "Archive", "All Mail"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Gmail lookup candidates = %#v, want %#v", got, want)
	}
}
