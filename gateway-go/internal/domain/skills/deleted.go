// deleted.go — persistent tombstones for deleted bundled skills.
//
// Mutable skills (managed/workspace/personal/...) are deleted by removing
// their directory, but bundled skills live in the repo's checked-in skills/
// tree: deleting files there would dirty the production checkout (and the
// auto-deploy git pull). Instead the delete RPC records the skill name here
// and every catalog surface (prompt, skills tab, slash routing) filters it
// out alongside the curator-archived set. Recovery is manual by design:
// remove the name from the JSON file (or delete the file) and restart or
// touch the catalog.
package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// deletedSkillsPath is the tombstone file, a sibling of the curator state so
// operator-facing skill lifecycle data stays in one place.
func deletedSkillsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", "deleted_skills.json")
}

type deletedSkillsFile struct {
	Skills []string `json:"skills"`
}

// LoadDeletedSkillNames returns the tombstoned skill names, nil when none.
// Best-effort by contract: an unreadable or malformed file reads as empty —
// deletion filtering must never break catalog discovery.
func LoadDeletedSkillNames() map[string]struct{} {
	path := deletedSkillsPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f deletedSkillsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil
	}
	if len(f.Skills) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(f.Skills))
	for _, n := range f.Skills {
		if n != "" {
			names[n] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// MarkSkillDeleted appends name to the tombstone file (idempotent, sorted for
// stable diffs). Used by the delete RPC for bundled skills.
func MarkSkillDeleted(name string) error {
	path := deletedSkillsPath()
	if path == "" {
		return os.ErrNotExist
	}
	names := LoadDeletedSkillNames()
	if _, ok := names[name]; ok {
		return nil
	}
	merged := make([]string, 0, len(names)+1)
	for n := range names {
		merged = append(merged, n)
	}
	merged = append(merged, name)
	sort.Strings(merged)
	data, err := json.MarshalIndent(deletedSkillsFile{Skills: merged}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, nil)
}
