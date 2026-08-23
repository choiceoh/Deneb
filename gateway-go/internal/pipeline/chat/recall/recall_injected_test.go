package recall

import (
	"reflect"
	"testing"
)

func TestInjectedPaths_ConsumeOnceAndClearOnEmpty(t *testing.T) {
	const session = "client:test-injected"
	StoreInjectedPaths(session, []string{"프로젝트/a.md", "프로젝트/b.md"})

	got := TakeInjectedPaths(session)
	if !reflect.DeepEqual(got, []string{"프로젝트/a.md", "프로젝트/b.md"}) {
		t.Fatalf("first take = %v", got)
	}
	if again := TakeInjectedPaths(session); again != nil {
		t.Errorf("second take = %v, want nil (consume-once)", again)
	}

	// A later turn that injects nothing must clear a pending slot so its
	// answer cannot be attributed to the earlier turn's paths.
	StoreInjectedPaths(session, []string{"프로젝트/a.md"})
	StoreInjectedPaths(session, nil)
	if got := TakeInjectedPaths(session); got != nil {
		t.Errorf("cleared slot take = %v, want nil", got)
	}

	// Empty session keys are ignored on both sides.
	StoreInjectedPaths("", []string{"프로젝트/a.md"})
	if got := TakeInjectedPaths(""); got != nil {
		t.Errorf("empty session take = %v, want nil", got)
	}
}

func TestMatchCitedPaths(t *testing.T) {
	paths := []string{
		"프로젝트/PRJ-021/한울읍성.md",
		"거래처/knk-energy.md",
		"프로젝트/PRJ-007/대표.md", // generic title — full path required
		"인물/김.md",            // 1-rune title — full path required
	}

	tests := []struct {
		name   string
		answer string
		want   []string
	}{
		{
			name:   "full path cited",
			answer: "상세는 프로젝트/PRJ-021/한울읍성.md 참조.",
			want:   []string{"프로젝트/PRJ-021/한울읍성.md"},
		},
		{
			name:   "path without md suffix cited",
			answer: "거래처/knk-energy 페이지에 정리돼 있습니다.",
			want:   []string{"거래처/knk-energy.md"},
		},
		{
			name:   "bare specific title cited",
			answer: "한울읍성 현장은 다음 주 계약 예정입니다.",
			want:   []string{"프로젝트/PRJ-021/한울읍성.md"},
		},
		{
			name:   "generic title alone is not a citation",
			answer: "대표 담당자가 확인했고, 김 부장이 회신했습니다.",
			want:   nil,
		},
		{
			name:   "unrelated answer cites nothing",
			answer: "오늘 일정은 오후 회의 하나입니다.",
			want:   nil,
		},
		{
			name:   "empty answer",
			answer: "",
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchCitedPaths(tt.answer, paths, nil); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("matchCitedPaths(%q, nil) = %v, want %v", tt.answer, got, tt.want)
			}
		})
	}
}
