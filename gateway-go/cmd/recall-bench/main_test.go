package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathHitRequiresSegmentBoundary(t *testing.T) {
	tests := []struct {
		name, gold, path string
		want             bool
	}{
		{"exact", "프로젝트/영덕.md", "프로젝트/영덕.md", true},
		{"suffix expansion", "비금도-154kv", "프로젝트/비금도-154kv-케이블.md", true},
		{"nested segment", "영덕", "프로젝트/영덕/대표.md", true},
		{"substring is not a segment", "영덕", "프로젝트/남영덕/대표.md", false},
		{"empty", "", "프로젝트/영덕.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathHit(tt.gold, tt.path); got != tt.want {
				t.Fatalf("pathHit(%q, %q) = %v", tt.gold, tt.path, got)
			}
		})
	}
}

func TestLoadGoldSkipsCommentsAndCountsMalformedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gold.jsonl")
	data := "# header\n\n" +
		`{"id":"one","question":"q","gold_paths":["a.md"]}` + "\n" +
		"not-json\n" +
		`{"id":"two","question":"q2","gold_paths":["b.md"]}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, malformed, err := loadGold(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || malformed != 1 || cases[1].ID != "two" {
		t.Fatalf("cases=%#v malformed=%d", cases, malformed)
	}
}
