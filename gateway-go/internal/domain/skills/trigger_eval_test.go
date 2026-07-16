package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const minTriggerEvalCasesPerClass = 5

type triggerEvalFixture struct {
	Skill string            `json:"skill"`
	Cases []triggerEvalCase `json:"cases"`
}

type triggerEvalCase struct {
	Name          string `json:"name"`
	Message       string `json:"message"`
	ShouldTrigger bool   `json:"shouldTrigger"`
}

// TestBundledSkillTriggerEvals is the changed-skill CI gate. It reads the real
// bundled SKILL.md frontmatter and exercises the production trigger matcher,
// so changing either a trigger or its positive/negative corpus cannot silently
// regress routing. go-test uses -count=1 because these sidecars are runtime
// files rather than Go compiler inputs.
func TestBundledSkillTriggerEvals(t *testing.T) {
	repoRoot := triggerEvalRepoRoot(t)
	skillsRoot := filepath.Join(repoRoot, "skills")

	resolved := make([]PromptSkill, 0)
	fixturePaths := make(map[string]string)
	err := filepath.WalkDir(skillsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		frontmatter := ParseFrontmatter(string(content))
		metadata := ResolveDenebMetadata(frontmatter)
		if metadata == nil || len(metadata.Triggers) == 0 {
			return nil
		}
		name := frontmatter["name"]
		if name == "" {
			name = filepath.Base(filepath.Dir(path))
		}
		invocation := ResolveSkillInvocationPolicy(frontmatter)
		resolved = append(resolved, PromptSkill{
			Name:                   name,
			Triggers:               metadata.Triggers,
			DisableModelInvocation: invocation.DisableModelInvocation,
		})
		fixturePaths[name] = filepath.Join(filepath.Dir(path), "evals", "trigger_cases.json")
		return nil
	})
	if err != nil {
		t.Fatalf("walk bundled skills: %v", err)
	}
	if len(resolved) == 0 {
		t.Fatal("expected bundled skills with metadata.deneb.triggers")
	}
	orphanFixtures, err := filepath.Glob(filepath.Join(skillsRoot, "*", "*", "evals", "trigger_cases.json"))
	if err != nil {
		t.Fatalf("glob trigger eval fixtures: %v", err)
	}
	for _, path := range orphanFixtures {
		fixture := loadTriggerEvalFixture(t, path)
		if expectedPath, exists := fixturePaths[fixture.Skill]; !exists || expectedPath != path {
			t.Errorf("orphan trigger fixture %s for skill %q without matching triggers", path, fixture.Skill)
		}
	}

	for _, skill := range resolved {
		t.Run(skill.Name, func(t *testing.T) {
			fixture := loadTriggerEvalFixture(t, fixturePaths[skill.Name])
			if fixture.Skill != skill.Name {
				t.Fatalf("fixture skill = %q, want %q", fixture.Skill, skill.Name)
			}
			positive, negative := 0, 0
			seenNames := make(map[string]struct{}, len(fixture.Cases))
			for _, testCase := range fixture.Cases {
				if testCase.Name == "" || testCase.Message == "" {
					t.Fatalf("case needs non-empty name and message: %+v", testCase)
				}
				if _, exists := seenNames[testCase.Name]; exists {
					t.Fatalf("duplicate case name %q", testCase.Name)
				}
				seenNames[testCase.Name] = struct{}{}
				matched := triggerEvalContains(MatchSkillTriggers(testCase.Message, resolved, 0), skill.Name)
				if matched != testCase.ShouldTrigger {
					t.Errorf("%s: matched=%t, want %t for %q", testCase.Name, matched, testCase.ShouldTrigger, testCase.Message)
				}
				if testCase.ShouldTrigger {
					positive++
				} else {
					negative++
				}
			}
			if positive < minTriggerEvalCasesPerClass || negative < minTriggerEvalCasesPerClass {
				t.Fatalf("need at least %d positive and %d negative cases, got %d/%d",
					minTriggerEvalCasesPerClass, minTriggerEvalCasesPerClass, positive, negative)
			}
		})
	}
}

func triggerEvalRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "skills")); statErr == nil && info.IsDir() {
			if _, statErr = os.Stat(filepath.Join(dir, "gateway-go", "go.mod")); statErr == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func loadTriggerEvalFixture(t *testing.T, path string) triggerEvalFixture {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trigger eval fixture %s: %v", path, err)
	}
	var fixture triggerEvalFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("parse trigger eval fixture %s: %v", path, err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatalf("trigger eval fixture %s has no cases", path)
	}
	return fixture
}

func triggerEvalContains(matches []PromptSkill, skillName string) bool {
	for _, match := range matches {
		if match.Name == skillName {
			return true
		}
	}
	return false
}

func TestMatchSkillTriggersHonorsSpecificityCapAndInvocationGate(t *testing.T) {
	skills := []PromptSkill{
		{Name: "short", Triggers: []string{"계약"}},
		{Name: "long", Triggers: []string{"공급계약"}},
		{Name: "disabled", Triggers: []string{"공급계약서"}, DisableModelInvocation: true},
	}
	got := MatchSkillTriggers("공급계약서를 봐줘", skills, 1)
	if len(got) != 1 || got[0].Name != "long" {
		t.Fatalf("specificity/cap mismatch: %s", fmt.Sprint(got))
	}
}
