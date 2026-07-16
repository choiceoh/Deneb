package skills

import "testing"

func TestFilterExcludedSkillsDeletesNamedEntries(t *testing.T) {
	entries := []SkillEntry{
		{Skill: Skill{Name: "active"}},
		{Skill: Skill{Name: "archived"}},
	}
	got := FilterExcludedSkills(entries, map[string]struct{}{"archived": {}})
	if len(got) != 1 || got[0].Skill.Name != "active" {
		t.Fatalf("unexpected filtered entries: %+v", got)
	}
}

func TestEntriesToPromptSkillsCarriesBodyInMemory(t *testing.T) {
	got := entriesToPromptSkills([]SkillEntry{{
		Skill: Skill{Name: "alpha", FilePath: "/skills/alpha/SKILL.md"},
		Body:  "# Alpha instructions",
	}})
	if len(got) != 1 || got[0].Body != "# Alpha instructions" {
		t.Fatalf("prompt skill body not propagated: %#v", got)
	}
}
