package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPlan(t *testing.T) {
	if plan, err := loadPlan(""); err != nil || plan != nil {
		t.Fatalf("empty plan = %#v, %v", plan, err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	data := `[{"op":"move","source":"old.md","target":"new.md","note":"tidy"}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := loadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Source != "old.md" || plan[0].Target != "new.md" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestLoadPlanReportsReadAndParseContext(t *testing.T) {
	if _, err := loadPlan(filepath.Join(t.TempDir(), "missing.json")); err == nil || !strings.Contains(err.Error(), "read plan") {
		t.Fatalf("read error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlan(path); err == nil || !strings.Contains(err.Error(), "parse plan") {
		t.Fatalf("parse error = %v", err)
	}
}
