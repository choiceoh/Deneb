package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
)

func TestBuildRecallSnapshotDoesNotReuseEllipticalFactAcrossPriorSubjects(t *testing.T) {
	chatrecall.ClearAll()
	t.Cleanup(chatrecall.ClearAll)
	root := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	const (
		alphaValue = "ALPHA-CURRENT-4821"
		betaValue  = "BETA-CURRENT-7312"
		followup   = "그거 뭐였지?"
		session    = "client:elliptical-fact-cache"
	)
	for _, input := range []wiki.FactInput{
		{Subject: "project:alpha", Key: "quote.amount", Value: alphaValue, Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityDirectUser},
		{Subject: "project:beta", Key: "quote.amount", Value: betaValue, Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityDirectUser},
	} {
		if _, err := store.UpsertFact(input); err != nil {
			t.Fatal(err)
		}
	}

	history := transcript.NewMemoryTranscriptStore()
	appendUser := func(text string, at int64) {
		t.Helper()
		if err := history.Append(session, toolport.NewTextChatMessage("user", text, at)); err != nil {
			t.Fatal(err)
		}
	}
	deps := runDeps{
		memory:     MemoryDeps{Wiki: store},
		transcript: history,
		logger:     discardLogger(),
	}

	appendUser("alpha 프로젝트 견적을 확인해줘", 1000)
	appendUser(followup, 2000)
	first := buildRecallSnapshot(context.Background(), RunParams{SessionKey: session, Message: followup}, deps, deps.logger)
	if !strings.Contains(first, alphaValue) || strings.Contains(first, betaValue) {
		t.Fatalf("alpha contextual recall mismatch: %q", first)
	}

	appendUser("beta 프로젝트 견적을 확인해줘", 3000)
	appendUser(followup, 4000)
	second := buildRecallSnapshot(context.Background(), RunParams{SessionKey: session, Message: followup}, deps, deps.logger)
	if !strings.Contains(second, betaValue) || strings.Contains(second, alphaValue) {
		t.Fatalf("elliptical cache reused alpha in beta context: %q", second)
	}
	if _, cached := chatrecall.CachedSnapshot(session, chatrecall.CueFingerprint(followup)); cached {
		t.Fatal("context-dependent follow-up was frozen under its raw cue")
	}
}
