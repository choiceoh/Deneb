package wiki

import (
	"strings"
	"testing"
)

func TestDroppedEnumNotes_ReportsInvalidStage(t *testing.T) {
	notes := DroppedEnumNotes("검토중", nil)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0], "검토중") || !strings.Contains(notes[0], "계약협의") {
		t.Errorf("note should name the dropped value and the vocabulary: %s", notes[0])
	}
}

func TestDroppedEnumNotes_ValidValuesSilent(t *testing.T) {
	if notes := DroppedEnumNotes("계약협의", []string{"태양광/루프탑", "기자재/케이블"}); len(notes) != 0 {
		t.Errorf("valid stage/kinds must produce no notes, got %v", notes)
	}
	if notes := DroppedEnumNotes("", nil); len(notes) != 0 {
		t.Errorf("empty inputs must produce no notes, got %v", notes)
	}
}

func TestDroppedEnumNotes_NormalizationIsNotAnError(t *testing.T) {
	// Synonym folding (모듈→기자재/모듈) and stage-word folding (개발→태양광)
	// persist a canonical value, so they must not be reported as drops.
	if notes := DroppedEnumNotes("", []string{"모듈", "개발", "BESS"}); len(notes) != 0 {
		t.Errorf("folded synonyms must produce no notes, got %v", notes)
	}
}

func TestDroppedEnumNotes_ReportsUnknownKinds(t *testing.T) {
	notes := DroppedEnumNotes("", []string{"블록체인", "태양광"})
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0], "블록체인") {
		t.Errorf("note should name the dropped kind: %s", notes[0])
	}
	if strings.Contains(notes[0], "태양광,") {
		t.Errorf("valid kind must not be listed as dropped: %s", notes[0])
	}
}

func TestRenderDefaultsUntypedPageType(t *testing.T) {
	// OKF v0.1 requires a non-empty type on every concept document.
	page := NewPage("무제", "업무", nil)
	if !strings.Contains(string(page.Render()), "type: concept") {
		t.Error("untyped page should render type: concept")
	}
	person := NewPage("홍길동", "인물", nil)
	if !strings.Contains(string(person.Render()), "type: entity") {
		t.Error("untyped 인물 page should render type: entity")
	}
	typed := NewPage("현장", "프로젝트", nil)
	typed.Meta.Type = "site"
	if !strings.Contains(string(typed.Render()), "type: site") {
		t.Error("explicit type must be preserved")
	}
}
