package prompts

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreSetResetAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-overrides.json")
	templates := []Template{{
		ID:          "mail.auto.analysis",
		Title:       "자동 메일 분석",
		Description: "메일 분석 지침",
		Category:    "메일",
		DefaultText: "default prompt",
		Editable:    true,
	}}
	store := NewStore(path, templates)

	entry, ok, err := store.Get("mail.auto.analysis")
	if err != nil || !ok {
		t.Fatalf("Get default ok=%v err=%v", ok, err)
	}
	if entry.Text != "default prompt" || entry.Overridden {
		t.Fatalf("default entry = %+v", entry)
	}

	entry, err = store.Set("mail.auto.analysis", " custom prompt ")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if entry.Text != "custom prompt" || !entry.Overridden || entry.UpdatedAtMs == 0 {
		t.Fatalf("set entry = %+v", entry)
	}

	reloaded := NewStore(path, templates)
	if got := reloaded.Text("mail.auto.analysis"); got != "custom prompt" {
		t.Fatalf("reloaded Text = %q", got)
	}

	entry, err = reloaded.Reset("mail.auto.analysis")
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if entry.Text != "default prompt" || entry.Overridden || entry.UpdatedAtMs != 0 {
		t.Fatalf("reset entry = %+v", entry)
	}
}

func TestStoreValidation(t *testing.T) {
	store := NewStore("", []Template{
		{ID: "editable", Title: "Editable", DefaultText: "default", Editable: true},
		{ID: "readonly", Title: "Read only", DefaultText: "default", Editable: false},
	})
	if _, err := store.Set("editable", " "); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty Set err = %v, want ErrEmpty", err)
	}
	if _, err := store.Set("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Set err = %v, want ErrNotFound", err)
	}
	if _, err := store.Set("readonly", "x"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("readonly Set err = %v, want ErrReadOnly", err)
	}
	if _, err := store.Reset("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Reset err = %v, want ErrNotFound", err)
	}
}
