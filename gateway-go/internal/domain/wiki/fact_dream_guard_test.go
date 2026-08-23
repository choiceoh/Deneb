package wiki

import (
	"log/slog"
	"testing"
)

func TestPrepareDreamUpdateRejectsFactProjection(t *testing.T) {
	wd := &WikiDreamer{logger: slog.Default()}
	_, ok := wd.prepareDreamUpdate(wikiUpdate{
		Action: "update", Path: factProfilePagePath, Title: "사용자 현행 사실",
		Category: "사용자", Content: "- 예전 값 되살리기",
	})
	if ok {
		t.Fatal("dreamer must not write the fact-plane projection")
	}
}
