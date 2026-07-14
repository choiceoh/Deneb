package toolport

import (
	"reflect"
	"testing"
)

// TestActivationNoticeRoundTrip: both writer formats parse back to the exact
// name list — the lockstep contract deferred_replay.go depends on.
func TestActivationNoticeRoundTrip(t *testing.T) {
	names := []string{"graphify", "notebook", "honcho:search"}
	for _, notice := range []string{
		FormatFetchActivationNotice(names),
		FormatSkillActivationNotice(names),
		FormatAlreadyActiveNotice(names),
	} {
		if got := ParseActivationNotices(notice); !reflect.DeepEqual(got, names) {
			t.Errorf("round-trip %q = %v, want %v", notice, got, names)
		}
	}
}

// TestActivationNoticeParsesEmbeddedContent: notices are parsed out of surrounding tool
// output (schema text before the fetch sentence, SKILL.md body before the
// skill notice), and legacy transcripts with the pre-replay fetch_tools
// wording still parse.
func TestActivationNoticeParsesEmbeddedContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			"fetch_tools result with schema text",
			"## graphify\ndesc\n```json\n{}\n```\n\nActivated 1 tool(s): graphify. You can now call them directly.",
			[]string{"graphify"},
		},
		{
			"skill notice appended to a read result",
			"[File: skills/x/SKILL.md | 9 lines]\n---\nname: x\n---\n\n[스킬 필요 도구 활성화: notebook — 스키마가 로드되어 fetch_tools 없이 바로 호출할 수 있습니다.]",
			[]string{"notebook"},
		},
		{"no notice", "총 3건의 메일이 있습니다.", nil},
		{"prose mentioning tool(s) without the full frame", "I activated 2 tool(s) yesterday.", nil},
		{
			"non-name tokens are dropped",
			"Activated 2 tool(s): graphify, NOT A NAME. You can now call them directly.",
			[]string{"graphify"},
		},
	}
	for _, tc := range cases {
		if got := ParseActivationNotices(tc.content); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestDeferredActivationSeedAndActivateMergeWithoutDuplicates: seeded names are immediately visible to both
// IsActive (tool goroutine view) and ActivatedNames (executor view), and merge
// with later Activate calls without duplication.
func TestDeferredActivationSeedAndActivateMergeWithoutDuplicates(t *testing.T) {
	da := NewDeferredActivation()
	da.Seed([]string{"graphify", "notebook"})
	if !da.IsActive("graphify") || !da.IsActive("notebook") {
		t.Fatal("seeded names must be active before any drain")
	}
	da.Activate([]string{"notebook", "process"})
	got := da.ActivatedNames()
	want := []string{"graphify", "notebook", "process"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActivatedNames = %v, want %v", got, want)
	}
}
