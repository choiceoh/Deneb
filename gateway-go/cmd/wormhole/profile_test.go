package main

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEntry_Dsv4ProfileSetsThinkingToggle(t *testing.T) {
	entry := normalizeEntry(modelEntry{Name: "dsv4-nothink", Profile: "dsv4", ThinkingMode: thinkingModeOff})
	if entry.ToggleKwarg != "thinking" {
		t.Fatalf("ToggleKwarg = %q, want thinking", entry.ToggleKwarg)
	}
}

func TestApplyDsv4Profile_InjectsSamplingWhenThinkingOff(t *testing.T) {
	entry := modelEntry{Name: "dsv4-nothink", Profile: "dsv4", ThinkingMode: thinkingModeOff}
	body := []byte(`{"model":"dsv4-nothink","messages":[{"role":"user","content":"hi"}]}`)
	out := applyDsv4Profile(entry, body)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["temperature"] != 0.6 {
		t.Errorf("temperature = %v, want 0.6", m["temperature"])
	}
	if m["top_p"] != 0.95 {
		t.Errorf("top_p = %v, want 0.95", m["top_p"])
	}
}

func TestApplyDsv4Profile_SkipsSamplingWhenThinkingStaysOn(t *testing.T) {
	entry := modelEntry{Name: "dsv4", Profile: "dsv4", ToggleKwarg: "thinking"}
	body := []byte(`{"model":"dsv4","messages":[{"role":"user","content":"이거 분석해줘"}]}`)
	out := applyDsv4Profile(entry, body)
	if string(out) != string(body) {
		t.Errorf("expected unchanged body while thinking stays on, got %s", out)
	}
}

func TestApplyDsv4Profile_PreservesExplicitSampling(t *testing.T) {
	entry := modelEntry{Name: "dsv4-nothink", Profile: "dsv4", ThinkingMode: thinkingModeOff}
	body := []byte(`{"model":"dsv4-nothink","temperature":0.2,"messages":[{"role":"user","content":"hi"}]}`)
	out := applyDsv4Profile(entry, body)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["temperature"] != 0.2 {
		t.Errorf("temperature = %v, want preserved 0.2", m["temperature"])
	}
	if m["top_p"] != 0.95 {
		t.Errorf("top_p = %v, want injected 0.95", m["top_p"])
	}
}
