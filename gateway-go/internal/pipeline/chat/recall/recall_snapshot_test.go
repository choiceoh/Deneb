package recall

import (
	"context"
	"testing"
	"time"
)

func TestBuildSnapshotServesCacheWithoutPhaseAndRearmsCitations(t *testing.T) {
	resetRecallSnapshotStore(t)
	t.Cleanup(ClearAll)

	const (
		session = "client:cached-recall"
		message = "alpha 계약 기억해?"
		block   = `<recall-context>
## 회상 근거 (자동 검색)
- source=wiki ref="프로젝트/alpha.md" confidence=high age=1h score=1.00
  match: alpha 계약 조건
</recall-context>`
	)
	storeSnapshot(session, cueFingerprint(message), block)

	phaseCalls := 0
	got := BuildSnapshot(
		context.Background(),
		Params{SessionKey: session, Message: message},
		Deps{},
		SnapshotOptions{OnExplicitRecall: func(time.Time) { phaseCalls++ }},
		nil,
	)
	if got != block {
		t.Fatalf("BuildSnapshot returned %q, want cached block", got)
	}
	if phaseCalls != 0 {
		t.Fatalf("cache hit emitted recall phase %d times", phaseCalls)
	}

	paths := takeInjectedPaths(session)
	if len(paths) != 1 || paths[0] != "프로젝트/alpha.md" {
		t.Fatalf("cached wiki refs were not re-armed for citation pass: %#v", paths)
	}
}
