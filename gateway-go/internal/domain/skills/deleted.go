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
	"strings"
	"time"

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
	Skills []DeletedSkill `json:"skills"`
}

// DeletedSkill is one tombstone. It carries WHY and WHEN, not just the name:
// the file used to be a bare name list, so a suppression left no trace of its
// reason and no clock on it. Six skills — including the RSI loop's own
// evolution-proposal and skill-factory — sat suppressed for five weeks before
// anyone noticed, and the only way to date it was the file's mtime
// (2026-08-26).
type DeletedSkill struct {
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
	At     string `json:"at,omitempty"` // RFC3339
}

// UnmarshalJSON accepts both shapes: the legacy bare string ("fact-check") and
// the object form. Old files stay readable — a tombstone that fails to parse
// would silently un-suppress a skill the operator deliberately hid.
func (d *DeletedSkill) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		d.Name = name
		return nil
	}
	type plain DeletedSkill
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*d = DeletedSkill(p)
	return nil
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
	for _, d := range f.Skills {
		if d.Name != "" {
			names[d.Name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// MarkSkillDeleted appends name to the tombstone file with the operator's
// reason and the current time (idempotent, sorted for stable diffs). Used by
// the delete RPC for bundled skills.
//
// The reason is recorded even when empty ("사유 미기재") because the alternative
// — a name with no context — is what made the 2026-08 suppressions
// unreviewable: nobody could tell whether a skill was hidden on purpose, by
// which lane, or how long ago.
func MarkSkillDeleted(name, reason string, now time.Time) error {
	path := deletedSkillsPath()
	if path == "" {
		return os.ErrNotExist
	}
	existing := LoadDeletedSkills()
	for _, d := range existing {
		if d.Name == name {
			return nil
		}
	}
	entry := DeletedSkill{Name: name, Reason: strings.TrimSpace(reason), At: now.UTC().Format(time.RFC3339)}
	if entry.Reason == "" {
		entry.Reason = "사유 미기재"
	}
	return writeDeletedSkills(path, append(existing, entry))
}

// LoadDeletedSkills returns every tombstone with its reason and time, sorted by
// name. Nil when none.
func LoadDeletedSkills() []DeletedSkill {
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
	out := make([]DeletedSkill, 0, len(f.Skills))
	for _, d := range f.Skills {
		if d.Name != "" {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func writeDeletedSkills(path string, entries []DeletedSkill) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	data, err := json.MarshalIndent(deletedSkillsFile{Skills: entries}, "", "  ")
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
	existing := LoadDeletedSkills()
	kept := make([]DeletedSkill, 0, len(existing))
	found := false
	for _, d := range existing {
		if d.Name == name {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	if !found {
		return nil
	}
	return writeDeletedSkills(path, kept)
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
