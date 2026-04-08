package polaris

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := testutil.Must(NewStore(filepath.Join(dir, "test.db")))
	t.Cleanup(func() { s.Close() })
	return s
}

func textMsg(role, text string, ts int64) toolctx.ChatMessage {
	b, _ := json.Marshal(text)
	return toolctx.ChatMessage{Role: role, Content: b, Timestamp: ts}
}

func TestAppendAndLoad(t *testing.T) {
	s := testStore(t)

	if err := s.AppendMessage("s1", textMsg("user", "hello", 1000)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage("s1", textMsg("assistant", "hi there", 2000)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage("s2", textMsg("user", "other session", 3000)); err != nil {
		t.Fatal(err)
	}

	// Count
	c1, _ := s.MessageCount("s1")
	if c1 != 2 {
		t.Fatalf("s1 count: got %d, want 2", c1)
	}
	c2, _ := s.MessageCount("s2")
	if c2 != 1 {
		t.Fatalf("s2 count: got %d, want 1", c2)
	}

	// Load all
	msgs := testutil.Must(s.LoadMessages("s1", 0, -1))
	if len(msgs) != 2 {
		t.Fatalf("load all: got %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("unexpected roles: %s, %s", msgs[0].Role, msgs[1].Role)
	}

	// Load range
	msgs = testutil.Must(s.LoadMessages("s1", 1, 1))
	if len(msgs) != 1 || msgs[0].Role != "assistant" {
		t.Fatalf("range load: got %d msgs, first role=%s", len(msgs), msgs[0].Role)
	}
}

func TestMsgIndexAutoIncrement(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 5; i++ {
		s.AppendMessage("s1", textMsg("user", "msg", int64(i*1000)))
	}

	maxIdx := testutil.Must(s.MaxMsgIndex("s1"))
	if maxIdx != 4 {
		t.Fatalf("max index: got %d, want 4", maxIdx)
	}
}

func TestSummaryNodes(t *testing.T) {
	s := testStore(t)

	id1, err := s.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "leaf summary 1",
		TokenEst: 100, CreatedAt: 1000, MsgStart: 0, MsgEnd: 9,
	})
	testutil.NoError(t, err)

	id2, err := s.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "leaf summary 2",
		TokenEst: 120, CreatedAt: 2000, MsgStart: 10, MsgEnd: 19,
	})
	testutil.NoError(t, err)

	// Condensed node referencing first leaf
	_, err = s.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 2, Content: "condensed",
		TokenEst: 80, CreatedAt: 3000, MsgStart: 0, MsgEnd: 19,
		ParentID: &id1,
	})
	testutil.NoError(t, err)
	_ = id2

	// Load level 1 only
	leaves := testutil.Must(s.LoadSummaries("s1", 1))
	if len(leaves) != 2 {
		t.Fatalf("leaves: got %d, want 2", len(leaves))
	}

	// Load all levels
	all := testutil.Must(s.LoadSummaries("s1", 0))
	if len(all) != 3 {
		t.Fatalf("all summaries: got %d, want 3", len(all))
	}

	// LatestSummaryCoverage
	cov := testutil.Must(s.LatestSummaryCoverage("s1"))
	if cov != 19 {
		t.Fatalf("coverage: got %d, want 19", cov)
	}

	// No summaries for unknown session
	cov2, _ := s.LatestSummaryCoverage("unknown")
	if cov2 != -1 {
		t.Fatalf("empty coverage: got %d, want -1", cov2)
	}
}

func TestDeleteSession(t *testing.T) {
	s := testStore(t)

	s.AppendMessage("s1", textMsg("user", "hello", 1000))
	s.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "summary",
		TokenEst: 50, CreatedAt: 2000, MsgStart: 0, MsgEnd: 0,
	})

	if err := s.DeleteSession("s1"); err != nil {
		t.Fatal(err)
	}

	c, _ := s.MessageCount("s1")
	if c != 0 {
		t.Fatalf("after delete: msg count %d", c)
	}
	nodes, _ := s.LoadSummaries("s1", 0)
	if len(nodes) != 0 {
		t.Fatalf("after delete: summary count %d", len(nodes))
	}
}

func TestSessionTokens(t *testing.T) {
	s := testStore(t)

	s.AppendMessage("s1", textMsg("user", "hello world this is a test", 1000))
	s.AppendMessage("s1", textMsg("assistant", "response text here", 2000))

	tokens := testutil.Must(s.SessionTokens("s1"))
	if tokens <= 0 {
		t.Fatalf("tokens: got %d, want > 0", tokens)
	}
}

func TestNewStoreInvalidPath(t *testing.T) {
	_, err := NewStore("/nonexistent/deeply/nested/dir/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestStoreDirectoryCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "polaris.db")

	s := testutil.Must(NewStore(dbPath))
	s.Close()

	// Verify directory structure was created (path ending in .db is converted to polaris/ dir).
	polarisDir := filepath.Join(dir, "polaris")
	if _, err := os.Stat(filepath.Join(polarisDir, "messages")); os.IsNotExist(err) {
		t.Fatal("messages directory not created")
	}
	if _, err := os.Stat(filepath.Join(polarisDir, "summaries")); os.IsNotExist(err) {
		t.Fatal("summaries directory not created")
	}
}
