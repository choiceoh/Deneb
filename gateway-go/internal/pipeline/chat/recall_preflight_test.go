package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestShouldRunRecallPreflight(t *testing.T) {
	if !shouldRunRecallPreflight("전에 이야기한 회상 개선 계속해줘") {
		t.Fatal("expected explicit recall cue to trigger preflight")
	}
	if shouldRunRecallPreflight("오늘 날씨 알려줘") {
		t.Fatal("did not expect ordinary message to trigger preflight")
	}
}

func TestRecallSearchQueriesDropsCueNoise(t *testing.T) {
	queries := recallSearchQueries("전에 Deneb 회상 개선 얘기했던 거 계속해줘")
	joined := strings.Join(queries, " ")
	if !strings.Contains(joined, "deneb") || !strings.Contains(joined, "개선") {
		t.Fatalf("expected high-signal terms in queries, got %v", queries)
	}
	if strings.Contains(joined, "전에") || strings.Contains(joined, "계속") {
		t.Fatalf("expected recall cue words to be removed, got %v", queries)
	}
}

func TestRecallSearchQueriesNormalizesKoreanEndings(t *testing.T) {
	queries := recallSearchQueries("전에 Deneb 회상 개선해줘")
	joined := strings.Join(queries, " ")
	if !strings.Contains(joined, "개선") {
		t.Fatalf("expected normalized Korean term 개선, got %v", queries)
	}
	if strings.Contains(joined, "개선해줘") {
		t.Fatalf("expected noisy verb ending to be stripped, got %v", queries)
	}
}

func TestBuildRecallPreflightInjectsWikiEvidence(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	page := &wiki.Page{
		Meta: wiki.Frontmatter{
			Title:      "Deneb 회상 개선 계획",
			Summary:    "회상 preflight와 위키 검색 개선",
			Category:   "프로젝트",
			Tags:       []string{"deneb", "recall"},
			Importance: 0.9,
		},
		Body: "응답 생성 전에 서버가 위키, 일지, 세션 이력을 검색해 근거를 주입한다.",
	}
	if err := store.WritePage("프로젝트/deneb-recall.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	out := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "전에 Deneb 회상 개선 얘기했던 거 계속해줘"},
		runDeps{wikiStore: store},
		nil,
	)
	if !strings.Contains(out, "회상 근거") {
		t.Fatalf("expected recall section, got %q", out)
	}
	if !strings.Contains(out, "프로젝트/deneb-recall.md") {
		t.Fatalf("expected wiki evidence path, got %q", out)
	}
	if !strings.Contains(out, "회상 preflight와 위키 검색 개선") {
		t.Fatalf("expected wiki summary in evidence, got %q", out)
	}
}

func TestBuildRecallPreflightUsesRecentDiaryForTopiclessRecall(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.AppendDiary("Deneb 회상 preflight 방향을 서버 주입 방식으로 정했다."); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}

	out := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "아까 뭐였지?"},
		runDeps{wikiStore: store},
		nil,
	)
	if !strings.Contains(out, "diary-") || !strings.Contains(out, "preflight") {
		t.Fatalf("expected recent diary fallback evidence, got %q", out)
	}
}

func TestBuildRecallPreflightSearchesTranscript(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	if err := transcript.Append("telegram:1", NewTextChatMessage("user", "alpha 결정은 서버 preflight로 하기로 했다", 1000)); err != nil {
		t.Fatalf("Append old: %v", err)
	}
	if err := transcript.Append("telegram:1", NewTextChatMessage("user", "전에 alpha 결정 기억나?", 2000)); err != nil {
		t.Fatalf("Append current: %v", err)
	}

	out := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "전에 alpha 결정 기억나?"},
		runDeps{transcript: transcript},
		nil,
	)
	if !strings.Contains(out, "transcript") || !strings.Contains(out, "서버 preflight") {
		t.Fatalf("expected transcript evidence, got %q", out)
	}
}

func TestBuildRecallPreflightNoTrigger(t *testing.T) {
	out := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "새 기능 설계해줘"},
		runDeps{},
		nil,
	)
	if out != "" {
		t.Fatalf("expected no recall section, got %q", out)
	}
}
