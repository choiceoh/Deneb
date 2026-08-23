package recall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// TestNeedsContextRewriteGatesOnDeicticMarkerAndBrevity pins the gate: a
// deictic marker alone is not enough — a self-contained message keeps its own
// vocabulary (signal terms exceed the gate) and must not be rewritten.
func TestNeedsContextRewriteGatesOnDeicticMarkerAndBrevity(t *testing.T) {
	rewrites := []string{
		"그 현장 계약 조건은 어떻게 됐지?",
		"그거 어떻게 됐어?",
		"거기 용량이 몇 MW였지?",
		"그 사업 수익 구조가 어떻게 되지?",
		"아까 그거 다시 보여줘",
		"그 회사 근황 어때?",
	}
	keeps := []string{
		"금호타이어 곡성 리스 사업 진행 상황 알려줘",
		"아까 보낸 금호타이어 곡성 견적서 첨부파일과 회의록까지 정리해서 보내줘",
		"새 기능 설계해줘",
		"그리고 나서 다음 단계를 진행해줘",
	}
	for _, m := range rewrites {
		if !needsContextRewrite(m) {
			t.Fatalf("expected rewrite for %q", m)
		}
	}
	for _, m := range keeps {
		if needsContextRewrite(m) {
			t.Fatalf("expected no rewrite for %q", m)
		}
	}
}

// TestLastPriorUserTurnSkipsCurrentAndStripsTimestamp covers the transcript
// tail walk: persisted user turns carry a "[RFC3339] " prefix, the already
// persisted current message must be skipped, and assistant turns are ignored.
func TestLastPriorUserTurnSkipsCurrentAndStripsTimestamp(t *testing.T) {
	transcript := newTestTranscriptStore()
	mustAppend := func(role, text string, ts int64) {
		t.Helper()
		if err := transcript.Append("telegram:1", toolport.NewTextChatMessage(role, text, ts)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	mustAppend("user", "[2026-08-23T09:00:00+09:00] 금호타이어 곡성 리스 사업 검토해줘", 1000)
	mustAppend("assistant", "곡성 리스 사업 검토 결과입니다…", 1100)
	mustAppend("user", "그 현장 계약 조건은 어떻게 됐지?", 2000)

	prior := lastPriorUserTurn(Deps{Transcript: transcript}, "telegram:1", "그 현장 계약 조건은 어떻게 됐지?")
	if prior != "금호타이어 곡성 리스 사업 검토해줘" {
		t.Fatalf("expected timestamp-stripped prior user turn, got %q", prior)
	}

	if got := lastPriorUserTurn(Deps{Transcript: transcript}, "other-session", "그 현장 계약 조건은 어떻게 됐지?"); got != "" {
		t.Fatalf("expected empty prior for unknown session, got %q", got)
	}
	if got := lastPriorUserTurn(Deps{}, "telegram:1", "그 현장 계약 조건은 어떻게 됐지?"); got != "" {
		t.Fatalf("expected empty prior without transcript dep, got %q", got)
	}
}

// TestBuildRewritesEllipticalQueryWithPriorTurnContext is the money test: an
// elliptical follow-up whose own tokens cannot reach the wiki page must find
// it once the prior user turn is prepended — and must NOT find it when the
// transcript has no prior turn (proving the rewrite, not the tokens, caused it).
func TestBuildRewritesEllipticalQueryWithPriorTurnContext(t *testing.T) {
	newStore := func(t *testing.T) *wiki.Store {
		t.Helper()
		dir := t.TempDir()
		store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		page := &wiki.Page{
			Meta: wiki.Frontmatter{
				Title:      "금호타이어 곡성 농공단지 리스사업",
				Summary:    "곡성 농공단지 지붕태양광 20년 리스 사업",
				Category:   "프로젝트",
				Importance: 0.9,
			},
			Body: "20년 리스 기간에 월 정산 구조로 협의 중이며 임차료율은 4.5%안이다.",
		}
		if err := store.WritePage("프로젝트/pl2-kum-epc-001.md", page); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
		return store
	}
	const current = "그거 진행 어느 정도까지 됐어?"

	t.Run("with prior turn", func(t *testing.T) {
		transcript := newTestTranscriptStore()
		if err := transcript.Append("telegram:1", toolport.NewTextChatMessage("user", "금호타이어 곡성 리스 사업 검토해줘", 1000)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := transcript.Append("telegram:1", toolport.NewTextChatMessage("user", current, 2000)); err != nil {
			t.Fatalf("Append current: %v", err)
		}
		out, _ := Build(
			context.Background(),
			Params{SessionKey: "telegram:1", Message: current},
			Deps{Wiki: newStore(t), Transcript: transcript},
			nil,
		)
		if !strings.Contains(out, "pl2-kum-epc-001") {
			t.Fatalf("expected rewritten query to surface the 곡성 page, got %q", out)
		}
	})

	t.Run("without prior turn", func(t *testing.T) {
		out, _ := Build(
			context.Background(),
			Params{SessionKey: "telegram:1", Message: current},
			Deps{Wiki: newStore(t)},
			nil,
		)
		if strings.Contains(out, "pl2-kum-epc-001") {
			t.Fatalf("elliptical query alone must not reach the page, got %q", out)
		}
	})
}
