package wiki

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestFactJournalAmbiguityNotifiesFatalObserverOnce(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	fatal := make(chan error, 2)
	store.SetFactJournalFailureObserver(func(err error) { fatal <- err })
	store.factJournalAppend = func(path string, record []byte) (factJournalAppendOutcome, error) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return factJournalAppendNotStarted, err
		}
		_, _ = f.Write(record[:len(record)/2])
		_ = f.Close()
		return factJournalAppendAmbiguous, fmt.Errorf("injected ambiguous append")
	}

	input := FactInput{
		Subject: "self", Key: "communication.language", Value: "한국어",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}
	if _, err := store.UpsertFact(input); err == nil {
		t.Fatal("ambiguous append unexpectedly succeeded")
	}
	select {
	case err := <-fatal:
		if !strings.Contains(err.Error(), "restart required") {
			t.Fatalf("fatal observer error=%v", err)
		}
	default:
		t.Fatal("ambiguous append did not notify fatal observer")
	}

	if _, err := store.UpsertFact(input); err == nil {
		t.Fatal("poisoned journal unexpectedly accepted a retry")
	}
	select {
	case err := <-fatal:
		t.Fatalf("poisoned retry notified observer again: %v", err)
	default:
	}
}
