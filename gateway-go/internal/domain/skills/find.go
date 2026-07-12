package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// FindSkillFile locates <name>/SKILL.md under root across the catalog layouts
// discovery understands: flat (root/<name>), category-nested
// (root/<category>/<name>), and the genesis two-level nesting
// (root/genesis/<category>/<name> — see DiscoverWorkspaceSkills). Returns the
// first existing path, probing shallow layouts before deep ones. Bounded to
// two directory levels and O(dirs) stats — meant for failure paths and record
// producers, not hot loops.
func FindSkillFile(root, name string) (string, bool) {
	root = strings.TrimSpace(root)
	name = strings.TrimSpace(name)
	if root == "" || name == "" {
		return "", false
	}
	if p := filepath.Join(root, name, "SKILL.md"); fileExists(p) {
		return p, true
	}
	cats, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, cat := range cats {
		if !cat.IsDir() {
			continue
		}
		if p := filepath.Join(root, cat.Name(), name, "SKILL.md"); fileExists(p) {
			return p, true
		}
	}
	for _, cat := range cats {
		if !cat.IsDir() {
			continue
		}
		subs, err := os.ReadDir(filepath.Join(root, cat.Name()))
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if !sub.IsDir() {
				continue
			}
			if p := filepath.Join(root, cat.Name(), sub.Name(), name, "SKILL.md"); fileExists(p) {
				return p, true
			}
		}
	}
	return "", false
}
