package wiki

import (
	"testing"
	"time"
)

func TestHasRecentRecallMiss(t *testing.T) {
	s := missStore(t)
	if s.HasRecentRecallMiss(time.Now(), time.Hour) {
		t.Fatal("empty ledger must read as no fresh demand")
	}
	if err := s.RecordRecallMiss(RecallMiss{Query: "완도 프로젝트"}); err != nil {
		t.Fatal(err)
	}
	if !s.HasRecentRecallMiss(time.Now(), time.Hour) {
		t.Error("a freshly recorded miss must read as fresh demand")
	}
	if s.HasRecentRecallMiss(time.Now().Add(2*time.Hour), time.Hour) {
		t.Error("a miss older than the window must not read as fresh")
	}
}
