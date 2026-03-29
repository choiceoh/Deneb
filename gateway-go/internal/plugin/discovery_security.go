package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CandidateBlockReason identifies why a candidate was blocked.
type CandidateBlockReason string

const (
	BlockSourceEscapesRoot   CandidateBlockReason = "source_escapes_root"
	BlockPathStatFailed      CandidateBlockReason = "path_stat_failed"
	BlockPathWorldWritable   CandidateBlockReason = "path_world_writable"
	BlockSuspiciousOwnership CandidateBlockReason = "path_suspicious_ownership"
)

type candidateBlockIssue struct {
	reason         CandidateBlockReason
	sourcePath     string
	rootPath       string
	targetPath     string
	sourceRealPath string
	rootRealPath   string
	modeBits       uint32
	foundUID       uint32
	expectedUID    uint32
}

func checkSourceEscapesRoot(source, rootDir string) *candidateBlockIssue {
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil
	}
	rootReal, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil
	}
	if isPathInside(rootReal, sourceReal) {
		return nil
	}
	return &candidateBlockIssue{
		reason:         BlockSourceEscapesRoot,
		sourcePath:     source,
		rootPath:       rootDir,
		targetPath:     source,
		sourceRealPath: sourceReal,
		rootRealPath:   rootReal,
	}
}

func isPathInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func checkPathStatAndPermissions(source, rootDir string, origin PluginOrigin, uid int) *candidateBlockIssue {
	paths := []string{rootDir, source}
	seen := make(map[string]bool)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true

		info, err := os.Stat(abs)
		if err != nil {
			return &candidateBlockIssue{
				reason:     BlockPathStatFailed,
				sourcePath: source,
				rootPath:   rootDir,
				targetPath: abs,
			}
		}

		// Check world-writable on Unix.
		mode := info.Mode().Perm()
		if mode&0o002 != 0 {
			// For bundled origins, attempt repair.
			if origin == OriginBundled {
				repaired := mode &^ 0o022
				if err := os.Chmod(abs, repaired); err == nil {
					info, err = os.Stat(abs)
					if err != nil {
						return &candidateBlockIssue{
							reason:     BlockPathStatFailed,
							sourcePath: source,
							rootPath:   rootDir,
							targetPath: abs,
						}
					}
					mode = info.Mode().Perm()
				}
			}
			if mode&0o002 != 0 {
				return &candidateBlockIssue{
					reason:     BlockPathWorldWritable,
					sourcePath: source,
					rootPath:   rootDir,
					targetPath: abs,
					modeBits:   uint32(mode),
				}
			}
		}

		// Check ownership for non-bundled origins.
		if origin != OriginBundled && uid >= 0 {
			if sysUID := fileUID(info); sysUID >= 0 && sysUID != uid && sysUID != 0 {
				return &candidateBlockIssue{
					reason:      BlockSuspiciousOwnership,
					sourcePath:  source,
					rootPath:    rootDir,
					targetPath:  abs,
					foundUID:    uint32(sysUID),
					expectedUID: uint32(uid),
				}
			}
		}
	}
	return nil
}

func findCandidateBlockIssue(source, rootDir string, origin PluginOrigin, uid int) *candidateBlockIssue {
	if issue := checkSourceEscapesRoot(source, rootDir); issue != nil {
		return issue
	}
	return checkPathStatAndPermissions(source, rootDir, origin, uid)
}

func formatCandidateBlockMessage(issue *candidateBlockIssue) string {
	switch issue.reason {
	case BlockSourceEscapesRoot:
		return fmt.Sprintf("blocked plugin candidate: source escapes plugin root (%s -> %s; root=%s)",
			issue.sourcePath, issue.sourceRealPath, issue.rootRealPath)
	case BlockPathStatFailed:
		return fmt.Sprintf("blocked plugin candidate: cannot stat path (%s)", issue.targetPath)
	case BlockPathWorldWritable:
		return fmt.Sprintf("blocked plugin candidate: world-writable path (%s, mode=%04o)",
			issue.targetPath, issue.modeBits)
	case BlockSuspiciousOwnership:
		return fmt.Sprintf("blocked plugin candidate: suspicious ownership (%s, uid=%d, expected uid=%d or root)",
			issue.targetPath, issue.foundUID, issue.expectedUID)
	default:
		return fmt.Sprintf("blocked plugin candidate: %s (%s)", issue.reason, issue.targetPath)
	}
}

func (d *PluginDiscoverer) isUnsafeCandidate(ctx *discoveryContext, source, rootDir string, origin PluginOrigin) bool {
	issue := findCandidateBlockIssue(source, rootDir, origin, ctx.ownershipUID)
	if issue == nil {
		return false
	}
	*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
		Level:   "warn",
		Source:  issue.targetPath,
		Message: formatCandidateBlockMessage(issue),
	})
	return true
}
