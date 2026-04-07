package textsearch

import (
	"testing"
)

func TestEmptyIndex(t *testing.T) {
	idx := New()
	hits := idx.Search("hello", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(hits))
	}
	if idx.Len() != 0 {
		t.Fatalf("expected len 0, got %d", idx.Len())
	}
}

func TestBasicSearch(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "The quick brown fox")
	idx.Upsert("doc2", "The lazy brown dog")
	idx.Upsert("doc3", "A completely unrelated document")

	hits := idx.Search("brown", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
}

func TestANDSearch(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "brown fox jumps")
	idx.Upsert("doc2", "brown dog sleeps")
	idx.Upsert("doc3", "white fox runs")

	// "brown fox" should only match doc1 (AND mode).
	hits := idx.Search("brown fox", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 AND hit, got %d", len(hits))
	}
	if hits[0].ID != "doc1" {
		t.Fatalf("expected doc1, got %s", hits[0].ID)
	}
}

func TestORFallback(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "apples and oranges")
	idx.Upsert("doc2", "bananas and grapes")

	// "apples bananas" has no AND match, should fall back to OR.
	hits := idx.Search("apples bananas", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 OR hits, got %d", len(hits))
	}
}

func TestHangulSearch(t *testing.T) {
	idx := New()
	idx.Upsert("page1", "DGX Spark 설정 가이드", "GPU 설정 방법을 설명합니다")
	idx.Upsert("page2", "네트워크 설정", "네트워크 인터페이스 구성")

	// Hangul prefix matching: "설정" should match both.
	hits := idx.Search("설정", 10)
	if len(hits) != 2 {
		t.Fatalf("expected 2 Hangul hits, got %d", len(hits))
	}
}

func TestUpsertReplace(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "old content here")

	hits := idx.Search("old", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for old, got %d", len(hits))
	}

	idx.Upsert("doc1", "new content here")

	hits = idx.Search("old", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits for old after update, got %d", len(hits))
	}

	hits = idx.Search("new", 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for new, got %d", len(hits))
	}
}

func TestRemove(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "hello world")
	idx.Remove("doc1")

	if idx.Len() != 0 {
		t.Fatalf("expected len 0 after remove, got %d", idx.Len())
	}

	hits := idx.Search("hello", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits after remove, got %d", len(hits))
	}
}

func TestClear(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "one")
	idx.Upsert("doc2", "two")
	idx.Clear()

	if idx.Len() != 0 {
		t.Fatalf("expected 0 after clear, got %d", idx.Len())
	}
}

func TestLimit(t *testing.T) {
	idx := New()
	for i := 0; i < 20; i++ {
		idx.Upsert(string(rune('a'+i)), "common word")
	}

	hits := idx.Search("common", 5)
	if len(hits) != 5 {
		t.Fatalf("expected 5 hits with limit, got %d", len(hits))
	}
}

func TestSnippet(t *testing.T) {
	idx := New()
	idx.Upsert("doc1", "This is a long document about Go programming and testing frameworks")

	hits := idx.Search("testing", 10)
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit")
	}
	if hits[0].Snippet == "" {
		t.Fatal("expected non-empty snippet")
	}
}

func TestScoreOrdering(t *testing.T) {
	idx := New()
	idx.Upsert("high", "fox fox fox fox fox") // high TF for "fox"
	idx.Upsert("low", "the fox and the dog")  // low TF for "fox"

	hits := idx.Search("fox", 10)
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "high" {
		t.Fatalf("expected 'high' first (higher TF), got %s", hits[0].ID)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatal("expected higher score for document with more term frequency")
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! 안녕하세요")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "hello" || tokens[1] != "world" || tokens[2] != "안녕하세요" {
		t.Fatalf("unexpected tokens: %v", tokens)
	}
}
