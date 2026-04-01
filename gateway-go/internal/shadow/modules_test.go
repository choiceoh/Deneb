package shadow

import "testing"

func TestDetectPush(t *testing.T) {
	tests := []struct {
		content  string
		detected bool
	}{
		{"git push origin main", true},
		{"푸시 완료했습니다", true},
		{"push 했어", true},
		{"그냥 일반 대화", false},
		{"pushed to remote", true},
	}
	for _, tt := range tests {
		_, got := detectPush(tt.content)
		if got != tt.detected {
			t.Errorf("detectPush(%q) = %v, want %v", tt.content, got, tt.detected)
		}
	}
}

func TestDetectTopic(t *testing.T) {
	tests := []struct {
		content string
		want    string
	}{
		{"telegram 채널 쪽 작업하자", "telegram"},
		{"memory store 관련 수정", "memory"},
		{"그냥 일반 대화", ""},
		{"vega search engine optimization", "vega"},
		{"테스트 추가해야 해", "테스트"},
	}
	for _, tt := range tests {
		got := detectTopic(tt.content)
		if got != tt.want {
			t.Errorf("detectTopic(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestDetectCodeChange(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"파일 수정했습니다", true},
		{"코드 변경이 필요합니다", true},
		{"리팩토링 완료", true},
		{"그냥 질문입니다", false},
		{"implemented the feature", true},
	}
	for _, tt := range tests {
		got := DetectCodeChange(tt.content)
		if got != tt.want {
			t.Errorf("DetectCodeChange(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestExtractFacts(t *testing.T) {
	tests := []struct {
		content string
		wantN   int
	}{
		{"나는 Go를 좋아하고 Rust도 선호합니다", 1},    // "좋아하" and "선호하" dedupe by overlap
		{"version 2.0 릴리스 준비", 1},               // "릴리스"
		{"그냥 일반 대화입니다", 0},
		{"we decided to use PostgreSQL", 1},        // "we decided"
	}
	for _, tt := range tests {
		facts := extractFacts(tt.content, "test")
		if len(facts) != tt.wantN {
			t.Errorf("extractFacts(%q) got %d facts, want %d", tt.content, len(facts), tt.wantN)
			for i, f := range facts {
				t.Logf("  fact[%d]: category=%s content=%q", i, f.Category, f.Content)
			}
		}
	}
}

func TestNormalizeErrorPattern(t *testing.T) {
	p1 := normalizeErrorPattern("connection refused at port 8080", "connection refused")
	p2 := normalizeErrorPattern("connection refused at port 9090", "connection refused")
	// After normalization, port numbers should be replaced with N.
	if p1 != p2 {
		t.Errorf("patterns should match after normalization: %q vs %q", p1, p2)
	}
}

func TestNormalizeTask(t *testing.T) {
	got := normalizeTask("  매일 아침  로그 확인  ")
	want := "매일 아침 로그 확인"
	if got != want {
		t.Errorf("normalizeTask = %q, want %q", got, want)
	}
}
