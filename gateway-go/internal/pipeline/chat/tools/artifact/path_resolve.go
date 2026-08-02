package artifact

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath resolves a potentially relative path against the default
// directory and clamps traversal outside that root.
func ResolvePath(path, defaultDir string) string {
	return ResolvePathWithRoots(path, defaultDir, nil)
}

// ResolvePathContained resolves like ResolvePath and additionally reports
// whether the requested path fell outside every allowed root and was clamped
// to the workspace root. Callers that surface errors need the flag: a clamped
// request hands them the workspace root (a directory), and describing THAT
// failure against the requested path invents facts — production heartbeats
// spent 15 edit calls on "SKILL.md is a directory" for a path that was a
// regular file all along (2026-08-02).
func ResolvePathContained(path, defaultDir string) (string, bool) {
	return resolvePathWithRoots(path, defaultDir, nil)
}

// ResolvePathWithRoots allows explicitly curated additional roots while still
// clamping every other path to the workspace. A leading ~ expands before the
// containment check.
func ResolvePathWithRoots(path, defaultDir string, extraRoots []string) string {
	resolved, _ := resolvePathWithRoots(path, defaultDir, extraRoots)
	return resolved
}

func resolvePathWithRoots(path, defaultDir string, extraRoots []string) (string, bool) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}

	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Clean(filepath.Join(defaultDir, path))
	}

	absDefault, err := filepath.Abs(defaultDir)
	if err != nil {
		return resolved, false
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return resolved, false
	}
	realResolved := evalPathForContainment(absResolved)
	realDefault := evalPathForContainment(absDefault)
	if pathUnderRoot(realResolved, realDefault) {
		return resolved, false
	}
	for _, root := range extraRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err == nil && pathUnderRoot(realResolved, evalPathForContainment(absRoot)) {
			return resolved, false
		}
	}
	return absDefault, true
}

// evalPathForContainment resolves symlinks in an existing path or in the
// nearest existing parent. The latter matters for write targets that do not yet
// exist: workspace/link/new.txt must still be recognized as escaping when
// workspace/link points outside the jail.
func evalPathForContainment(path string) string {
	path = filepath.Clean(path)
	probe := path
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return path
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

func pathUnderRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
