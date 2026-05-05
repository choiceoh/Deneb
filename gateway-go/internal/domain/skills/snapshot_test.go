package skills

import "testing"

func TestFilterExcludedSkills(t *testing.T) {
	entries := []SkillEntry{
		{Skill: Skill{Name: "active"}},
		{Skill: Skill{Name: "archived"}},
	}
	got := FilterExcludedSkills(entries, map[string]struct{}{"archived": {}})
	if len(got) != 1 || got[0].Skill.Name != "active" {
		t.Fatalf("unexpected filtered entries: %+v", got)
	}
}
