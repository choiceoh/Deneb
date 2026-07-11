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

// ResolvePathWithRoots allows explicitly curated additional roots while still
// clamping every other path to the workspace. A leading ~ expands before the
// containment check.
func ResolvePathWithRoots(path, defaultDir string, extraRoots []string) string {
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
		return resolved
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	realResolved := evalPathForContainment(absResolved)
	realDefault := evalPathForContainment(absDefault)
	if pathUnderRoot(realResolved, realDefault) {
		return resolved
	}
	for _, root := range extraRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err == nil && pathUnderRoot(realResolved, evalPathForContainment(absRoot)) {
			return resolved
		}
	}
	return absDefault
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
