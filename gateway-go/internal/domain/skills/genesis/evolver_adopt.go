// evolver_adopt.go — copy-on-evolve adoption of bundled (repo) skills.
//
// Bundled skills (the repo's skills/ tree) are deliberately NOT seeded into
// the genesis catalog: the curator's staleness archiver (90d) would archive
// the rarely-used ones wholesale, and the evolver must never rewrite the repo
// checkout in place — deploy.sh rsyncs skills/ with --delete on every deploy
// and the production checkout auto-pulls main, so an in-place edit is
// clobbered or conflicts. Instead, the first evolve verdict on a bundled
// skill ADOPTS it: the skill directory is copied into the managed dir
// (~/.deneb/skills/<name>), which overrides bundled at discovery precedence
// (bundled < managed — see skills/CLAUDE.md "Workspace Overrides Bundled"),
// and the evolution proceeds on the copy. First hit in production 2026-07-04:
// the review fork's evolve verdict on contract-review died with "not found in
// catalog" exactly here.
package genesis

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// SetAdoptionDirs configures bundled-skill adoption. bundledDir is where repo
// skills live (chat.BundledSkillsDir() in production); empty disables adoption
// and catalog misses stay hard errors. managedDir is where an adopted copy
// lands; empty falls back to skills.DefaultManagedSkillsDir() (~/.deneb/skills).
func (e *Evolver) SetAdoptionDirs(bundledDir, managedDir string) {
	e.configMu.Lock()
	e.bundledSkillsDir = bundledDir
	e.managedSkillsDir = managedDir
	e.configMu.Unlock()
}

func (e *Evolver) adoptionDirs() (bundled, managed string) {
	e.configMu.RLock()
	bundled, managed = e.bundledSkillsDir, e.managedSkillsDir
	e.configMu.RUnlock()
	if managed == "" {
		managed = skills.DefaultManagedSkillsDir()
	}
	return bundled, managed
}

// adoptBundledSkill copies the named bundled skill into the managed dir,
// registers the managed entry in the catalog, and returns it. Idempotent-ish:
// a managed copy that already exists on disk (a prior adoption, or a manual
// override that never entered this catalog) is loaded and registered as-is.
func (e *Evolver) adoptBundledSkill(name string) (*skills.SkillEntry, error) {
	bundledDir, managedDir := e.adoptionDirs()
	if bundledDir == "" {
		return nil, fmt.Errorf("no bundled skills dir configured")
	}
	dst := filepath.Join(managedDir, name)

	// Existing managed copy → just materialize its entry.
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err == nil {
		entry, lerr := skills.LoadSkillEntry(dst, skills.SourceManaged)
		if lerr != nil {
			return nil, fmt.Errorf("existing managed copy unreadable: %w", lerr)
		}
		if e.catalog != nil {
			e.catalog.Register(*entry)
		}
		return entry, nil
	}

	src, err := locateBundledSkillDir(bundledDir, name)
	if err != nil {
		return nil, err
	}
	if err := copyDirRecursive(src, dst); err != nil {
		// Best-effort cleanup: a half-copied skill dir would shadow the intact
		// bundled original at every future discovery.
		_ = os.RemoveAll(dst)
		return nil, fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	entry, err := skills.LoadSkillEntry(dst, skills.SourceManaged)
	if err != nil {
		_ = os.RemoveAll(dst)
		return nil, fmt.Errorf("adopted copy unreadable: %w", err)
	}
	if e.catalog != nil {
		e.catalog.Register(*entry)
	}
	if e.logger != nil {
		e.logger.Info("evolver: adopted bundled skill into managed dir",
			"skill", name, "from", src, "to", dst)
	}
	return entry, nil
}

// locateBundledSkillDir finds <root>/<name> (flat) or <root>/<category>/<name>
// (the repo's nested category layout) containing a SKILL.md.
func locateBundledSkillDir(root, name string) (string, error) {
	flat := filepath.Join(root, name)
	if hasSkillFile(flat) {
		return flat, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("bundled skills dir unreadable: %w", err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		nested := filepath.Join(root, ent.Name(), name)
		if hasSkillFile(nested) {
			return nested, nil
		}
	}
	return "", fmt.Errorf("skill %q not found under bundled dir %s", name, root)
}

func hasSkillFile(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !st.IsDir()
}

// copyDirRecursive copies the regular files and directories of src into dst
// (created as needed), preserving file permission bits. Symlinks and special
// files are skipped — a skill dir is SKILL.md plus references/templates/
// scripts/assets, all regular files.
func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		in, oerr := os.Open(p)
		if oerr != nil {
			return oerr
		}
		defer in.Close()
		out, cerr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if cerr != nil {
			return cerr
		}
		if _, werr := io.Copy(out, in); werr != nil {
			out.Close()
			return werr
		}
		return out.Close()
	})
}
