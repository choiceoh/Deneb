package pilot

import (
	"testing"
)

func TestExpandShortcuts(t *testing.T) {
	p := PilotParams{
		Task: "test",
		File: "main.go",
		Exec: "ls -la",
		Grep: "TODO",
		Path: "src/",
		Find: "*.go",
		URL:  "https://example.com",
	}

	specs := ExpandShortcuts(p)

	// Should have 5 sources: file, exec, grep, find, url.
	if len(specs) != 5 {
		t.Fatalf("expected 5 specs, got %d", len(specs))
	}

	// Verify tool names.
	tools := make([]string, len(specs))
	for i, s := range specs {
		tools[i] = s.Tool
	}

	expected := []string{"read", "exec", "grep", "find", "web_fetch"}
	for i, want := range expected {
		if tools[i] != want {
			t.Errorf("spec[%d].Tool = %q, want %q", i, tools[i], want)
		}
	}
}

func TestExpandShortcutsMultipleFiles(t *testing.T) {
	p := PilotParams{
		Task:  "test",
		Files: []string{"a.go", "b.go", "c.go"},
	}

	specs := ExpandShortcuts(p)
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}
	for _, s := range specs {
		if s.Tool != "read" {
			t.Errorf("expected tool 'read', got %q", s.Tool)
		}
	}
}

func TestExpandShortcuts_HTTP(t *testing.T) {
	p := PilotParams{Task: "analyze", HTTP: "https://api.example.com/data"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "http" {
		t.Errorf("expected 1 http spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_KVKey(t *testing.T) {
	p := PilotParams{Task: "check", KVKey: "config.theme"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "kv" {
		t.Errorf("expected 1 kv spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Memory(t *testing.T) {
	p := PilotParams{Task: "summarize", Memory: "배포 결정"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "memory" {
		t.Errorf("expected 1 memory spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Gmail(t *testing.T) {
	p := PilotParams{Task: "summarize", Gmail: "from:alice subject:회의"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "gmail" {
		t.Errorf("expected 1 gmail spec, got %d", len(specs))
	}
	if specs[0].Label != "gmail: from:alice subject:회의" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}
}

func TestExpandShortcuts_YouTube(t *testing.T) {
	p := PilotParams{Task: "summarize", YouTube: "https://youtube.com/watch?v=abc123"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "youtube_transcript" {
		t.Errorf("expected 1 youtube_transcript spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Polaris(t *testing.T) {
	p := PilotParams{Task: "explain", Polaris: "aurora context engine"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "polaris" {
		t.Errorf("expected 1 polaris spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Image(t *testing.T) {
	p := PilotParams{Task: "describe", Image: "/tmp/screenshot.png"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "image" {
		t.Errorf("expected 1 image spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_AllNew(t *testing.T) {
	p := PilotParams{
		Task:    "analyze everything",
		Gmail:   "invoice",
		YouTube: "https://youtube.com/watch?v=x",
		Polaris: "tools",
		Image:   "/tmp/img.png",
	}
	specs := ExpandShortcuts(p)
	if len(specs) != 4 {
		t.Fatalf("expected 4 specs, got %d", len(specs))
	}
	tools := make([]string, len(specs))
	for i, s := range specs {
		tools[i] = s.Tool
	}
	expected := []string{"gmail", "youtube_transcript", "polaris", "image"}
	for i, want := range expected {
		if tools[i] != want {
			t.Errorf("spec[%d].Tool = %q, want %q", i, tools[i], want)
		}
	}
}

func TestExpandShortcuts_Diff(t *testing.T) {
	// "all" mode.
	p := PilotParams{Task: "review", Diff: "all"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "diff" {
		t.Fatalf("expected 1 diff spec, got %d", len(specs))
	}
	if specs[0].Label != "diff: all" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// Commit hash mode.
	p2 := PilotParams{Task: "review", Diff: "abc123"}
	specs2 := ExpandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "diff" {
		t.Fatalf("expected 1 diff spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Test(t *testing.T) {
	// Specific path.
	p := PilotParams{Task: "analyze", Test: "gateway-go/..."}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "test" {
		t.Fatalf("expected 1 test spec, got %d", len(specs))
	}
	if specs[0].Label != "test: gateway-go/..." {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// "all" mode.
	p2 := PilotParams{Task: "analyze", Test: "all"}
	specs2 := ExpandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "test" {
		t.Fatalf("expected 1 test spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Tree(t *testing.T) {
	p := PilotParams{Task: "overview", Tree: "/home/user/project"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "tree" {
		t.Fatalf("expected 1 tree spec, got %d", len(specs))
	}
	if specs[0].Label != "tree: /home/user/project" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}
}

func TestExpandShortcuts_GitLog(t *testing.T) {
	// "recent" mode.
	p := PilotParams{Task: "summarize", GitLog: "recent"}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "git" {
		t.Fatalf("expected 1 git spec, got %d", len(specs))
	}
	if specs[0].Label != "git_log: recent" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// "oneline" mode.
	p2 := PilotParams{Task: "summarize", GitLog: "oneline"}
	specs2 := ExpandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "git" {
		t.Fatalf("expected 1 git spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Health(t *testing.T) {
	p := PilotParams{Task: "diagnose", Health: true}
	specs := ExpandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "health_check" {
		t.Fatalf("expected 1 health_check spec, got %d", len(specs))
	}
	if specs[0].Label != "health_check" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// Health=false should not add a spec.
	p2 := PilotParams{Task: "diagnose", Health: false}
	specs2 := ExpandShortcuts(p2)
	if len(specs2) != 0 {
		t.Errorf("expected 0 specs for health=false, got %d", len(specs2))
	}
}
