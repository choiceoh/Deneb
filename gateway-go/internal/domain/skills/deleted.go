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

// UnmarkSkillDeleted removes name from the tombstone file, making a bundled
// skill live again. Idempotent — unmarking a skill that was never deleted is a
// no-op, not an error.
//
// This exists because deletion of a BUNDLED skill was one-way. The repo tree is
// a production checkout so the files cannot be removed; the delete RPC records
// a tombstone instead, and the tombstoned skill then vanishes from every
// surface INCLUDING the list the operator would restore it from. Recovering
// meant hand-editing ~/.deneb/data/deleted_skills.json.
//
// It cost real capability twice: on 2026-08-18 the operator typed two exact
// triggers of kb-interview and nothing fired (tombstoned 07-21, nothing said
// so), and on 2026-08-26 an RSI runtime check found evolution-proposal and
// skill-factory — the loop's own proposal and skill-creation machinery —
// tombstoned without intent, alongside four others.
func UnmarkSkillDeleted(name string) error {
	path := deletedSkillsPath()
	if path == "" {
		return os.ErrNotExist
	}
	names := LoadDeletedSkillNames()
	if _, ok := names[name]; !ok {
		return nil
	}
	delete(names, name)
	merged := make([]string, 0, len(names))
	for n := range names {
		merged = append(merged, n)
	}
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

// DeletedSkillNamesSorted returns the tombstoned names in stable order, so a
// surface can SHOW what it suppressed. A deletion nobody can see is a deletion
// nobody can undo.
func DeletedSkillNamesSorted() []string {
	names := LoadDeletedSkillNames()
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
