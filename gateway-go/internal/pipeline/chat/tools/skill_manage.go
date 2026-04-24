// skill_manage.go implements the skill_manage agent tool, enabling the LLM
// to create, patch, read, and delete skills at runtime.
//
// Modeled after hermes-agent's skill_manage() tool: the LLM decides when to
// create or improve skills based on conversation context, and executes the
// change via this tool. No code-level complexity detection needed — the LLM
// handles the judgment call.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// SkillManageInvalidateFn is called after any skill write/delete to bust the
// skills prompt cache so changes take effect on the next turn.
type SkillManageInvalidateFn func()

// ToolSkills creates the unified skills tool with action dispatch (list/create/patch/delete/read/list_files).
func ToolSkills(getSnapshot SkillsSnapshotProvider, workspaceDir string, invalidate SkillManageInvalidateFn) ToolFunc {
	listFn := toolSkillsList(getSnapshot)
	manageFn := toolSkillManage(workspaceDir, invalidate)
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		switch p.Action {
		case "list":
			return listFn(ctx, input)
		case "create", "patch", "delete", "read", "list_files":
			return manageFn(ctx, input)
		default:
			return "action은 list, create, patch, delete, read, list_files 중 하나를 지정하세요.", nil
		}
	}
}

// toolSkillManage returns a tool that lets the LLM create, patch, read, and
// delete skills at runtime.
//
// Prompt-cache doctrine: a skill write would normally force a rebuild of
// the semi-static "skills prompt" block mid-conversation, which breaks
// the static/semi-static cache marker. To preserve cache hit rate, the
// default behavior is DEFERRED — the write hits disk and the registry
// but the system prompt is NOT rebuilt until the next session starts.
// Pass `apply: true` to opt into immediate invalidation when the agent
// truly needs the change visible in the current turn (rare).
func toolSkillManage(workspaceDir string, invalidate SkillManageInvalidateFn) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action      string `json:"action"`
			Name        string `json:"name"`
			Category    string `json:"category"`
			Content     string `json:"content"`
			OldText     string `json:"old_text"`
			NewText     string `json:"new_text"`
			FilePath    string `json:"file_path"`
			FileContent string `json:"file_content"`
			Apply       bool   `json:"apply"`
		}
		if err := jsonutil.UnmarshalInto("skill_manage params", input, &p); err != nil {
			return "", err
		}
		if p.Name == "" {
			return "", fmt.Errorf("name is required")
		}

		// Sanitize skill name: lowercase, hyphens only.
		p.Name = sanitizeSkillName(p.Name)
		effectiveInvalidate := cacheAwareInvalidate(invalidate, p.Apply)

		var (
			result string
			err    error
		)
		switch p.Action {
		case "create":
			result, err = skillCreate(workspaceDir, p.Name, p.Category, p.Content, effectiveInvalidate)
		case "patch":
			result, err = skillPatch(workspaceDir, p.Name, p.OldText, p.NewText, effectiveInvalidate)
		case "delete":
			result, err = skillDelete(workspaceDir, p.Name, effectiveInvalidate)
		case "read":
			return skillRead(workspaceDir, p.Name, p.FilePath)
		case "list_files":
			return skillListFiles(workspaceDir, p.Name)
		default:
			return "", fmt.Errorf("unknown action %q: use create, patch, delete, read, or list_files", p.Action)
		}
		if err != nil {
			return "", err
		}
		return result + cacheApplyNotice(p.Apply), nil
	}
}

// cacheApplyNotice returns a short Korean notice the agent sees after
// any mutating skill_manage action, making clear whether the change is
// visible in the current session or deferred to the next.
func cacheApplyNotice(apply bool) string {
	if apply {
		return " (apply=true: 이번 세션에 즉시 반영됩니다.)"
	}
	return " (변경은 저장됐고 다음 세션부터 반영됩니다. 지금 바로 반영하려면 apply=true 를 추가하세요.)"
}

// cacheAwareInvalidate returns the real invalidate function when apply=true
// (immediate cache bust), or a no-op otherwise so the prompt cache is
// preserved for the current session. A trailing notice in the tool
// response tells the agent that the change is deferred.
func cacheAwareInvalidate(inner SkillManageInvalidateFn, apply bool) SkillManageInvalidateFn {
	if apply {
		return inner
	}
	return func() { /* deferred: prompt cache stays warm until next session */ }
}

func skillCreate(workspaceDir, name, category, content string, invalidate SkillManageInvalidateFn) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for create")
	}
	if category == "" {
		return "", fmt.Errorf("category is required for create (coding, productivity, devops, integration)")
	}
	if !isValidCategory(category) {
		return "", fmt.Errorf("invalid category %q: use coding, productivity, devops, or integration", category)
	}

	// Validate that content has valid frontmatter.
	header, _ := skills.ExtractFrontmatterBlock(content)
	if header == "" {
		return "", fmt.Errorf("content must have valid YAML frontmatter (---\\nname: ...\\n---)")
	}
	fm := skills.ParseFrontmatter(content)
	if fm["name"] == "" {
		return "", fmt.Errorf("frontmatter must include 'name' field")
	}

	skillDir := filepath.Join(workspaceDir, "skills", category, name)
	skillPath := filepath.Join(skillDir, "SKILL.md")

	// Check if skill already exists.
	if _, err := os.Stat(skillPath); err == nil {
		return "", fmt.Errorf("skill %q already exists at %s; use patch to modify", name, skillPath)
	}

	// Create directory and write.
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create skill directory: %w", err)
	}
	if err := atomicfile.WriteFile(skillPath, []byte(content), nil); err != nil {
		return "", fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	invalidate()
	return fmt.Sprintf("Created skill %q at skills/%s/%s/SKILL.md", name, category, name), nil
}

func skillPatch(workspaceDir, name, oldText, newText string, invalidate SkillManageInvalidateFn) (string, error) {
	if oldText == "" {
		return "", fmt.Errorf("old_text is required for patch")
	}
	if newText == "" {
		return "", fmt.Errorf("new_text is required for patch")
	}

	skillPath, err := findSkillPath(workspaceDir, name)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("failed to read SKILL.md: %w", err)
	}
	content := string(data)

	if strings.Contains(content, oldText) {
		// Exact match — verify uniqueness.
		count := strings.Count(content, oldText)
		if count > 1 {
			return "", fmt.Errorf("old_text matches %d locations; make it more specific", count)
		}
		content = strings.Replace(content, oldText, newText, 1)
	} else {
		// Fuzzy: line-based matching absorbs indentation and trailing-space
		// differences. Each line is compared after TrimSpace so leading
		// indent and trailing whitespace are ignored. The matched original
		// lines are replaced with newText verbatim; the rest of the file is
		// preserved exactly.
		var err error
		content, err = fuzzyLineReplace(content, oldText, newText)
		if err != nil {
			return "", err
		}
	}

	// Validate the result still has valid frontmatter.
	header, _ := skills.ExtractFrontmatterBlock(content)
	if header == "" {
		return "", fmt.Errorf("patch would break SKILL.md structure (invalid frontmatter)")
	}

	if err := atomicfile.WriteFile(skillPath, []byte(content), nil); err != nil {
		return "", fmt.Errorf("failed to write patched SKILL.md: %w", err)
	}

	invalidate()
	return fmt.Sprintf("Patched skill %q", name), nil
}

func skillDelete(workspaceDir, name string, invalidate SkillManageInvalidateFn) (string, error) {
	skillPath, err := findSkillPath(workspaceDir, name)
	if err != nil {
		return "", err
	}
	skillDir := filepath.Dir(skillPath)

	if err := os.RemoveAll(skillDir); err != nil {
		return "", fmt.Errorf("failed to delete skill directory: %w", err)
	}

	invalidate()
	return fmt.Sprintf("Deleted skill %q", name), nil
}

func skillRead(workspaceDir, name, filePath string) (string, error) {
	baseSkillPath, err := findSkillPath(workspaceDir, name)
	if err != nil {
		return "", err
	}

	var targetPath string
	if filePath != "" {
		// Read auxiliary file within skill directory.
		skillDir := filepath.Dir(baseSkillPath)
		targetPath = filepath.Join(skillDir, filePath)
		// Prevent path traversal.
		if !isWithinDir(targetPath, skillDir) {
			return "", fmt.Errorf("file_path escapes skill directory")
		}
	} else {
		targetPath = baseSkillPath
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", targetPath, err)
	}
	return string(data), nil
}

func skillListFiles(workspaceDir, name string) (string, error) {
	skillPath, err := findSkillPath(workspaceDir, name)
	if err != nil {
		return "", err
	}
	skillDir := filepath.Dir(skillPath)

	var files []string
	err = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		rel, _ := filepath.Rel(skillDir, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			files = append(files, rel+"/")
		} else {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to list files: %w", err)
	}
	if len(files) == 0 {
		return "No files found.", nil
	}
	return strings.Join(files, "\n"), nil
}

// --- helpers ---

func findSkillPath(workspaceDir, name string) (string, error) {
	skillsRoot := filepath.Join(workspaceDir, "skills")

	// Search all category directories for the skill.
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return "", fmt.Errorf("skills directory not found: %w", err)
	}
	for _, cat := range entries {
		if !cat.IsDir() {
			continue
		}
		candidate := filepath.Join(skillsRoot, cat.Name(), name, "SKILL.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("skill %q not found in any category under skills/", name)
}

func isValidCategory(cat string) bool {
	switch cat {
	case "coding", "productivity", "devops", "integration":
		return true
	}
	return false
}

func sanitizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	// Remove anything that isn't alphanumeric or hyphen.
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fuzzyLineReplace matches oldText against content line-by-line after
// trimming whitespace, then replaces the matched original lines with newText.
// This absorbs indentation and trailing-space differences that commonly occur
// when an LLM regenerates text from memory.
func fuzzyLineReplace(content, oldText, newText string) (string, error) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldText, "\n")

	// Trim trailing empty lines from oldLines — LLM-generated text often
	// has a spurious trailing newline.
	for len(oldLines) > 0 && strings.TrimSpace(oldLines[len(oldLines)-1]) == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(oldLines) == 0 {
		return "", fmt.Errorf("old_text is empty after trimming")
	}

	matches := 0
	matchStart := -1
	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		found := true
		for j := range oldLines {
			if strings.TrimSpace(contentLines[i+j]) != strings.TrimSpace(oldLines[j]) {
				found = false
				break
			}
		}
		if found {
			matches++
			if matchStart < 0 {
				matchStart = i
			}
		}
	}

	if matches == 0 {
		return "", fmt.Errorf("old_text not found in SKILL.md (tried exact and fuzzy line matching)")
	}
	if matches > 1 {
		return "", fmt.Errorf("old_text matches %d locations with fuzzy matching; make it more specific", matches)
	}

	// Build result: lines before match + newText + lines after match.
	newLines := strings.Split(newText, "\n")
	result := make([]string, 0, len(contentLines)-len(oldLines)+len(newLines))
	result = append(result, contentLines[:matchStart]...)
	result = append(result, newLines...)
	result = append(result, contentLines[matchStart+len(oldLines):]...)
	return strings.Join(result, "\n"), nil
}

func isWithinDir(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absDir+string(filepath.Separator)) || absPath == absDir
}
