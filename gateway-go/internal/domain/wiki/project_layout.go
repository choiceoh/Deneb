// project_layout.go — the standardized per-project document layout, the single
// source of truth for "what is a project page" (2026-07 스키마 정형화).
//
// Every project lives in its own folder with fixed document slots:
//
//	프로젝트/<프로젝트명>/
//	  대표.md      — 대표페이지: 현재 상태·개요·핵심 사실 (digest/status/candidate target)
//	  로그.md      — 진행 로그: dated progress entries (events append here, NOT new pages)
//	  기자재/*.md  — equipment/material pages (cables, modules, quotes, spec sheets)
//	  메일분석/*.md — per-mail analysis raw pages (one page per Gmail message ID)
//
// Category-level (non-project) buckets under 프로젝트/:
//
//	프로젝트/거래/      — per-counterparty deal ledger pages (span projects)
//	프로젝트/메일분석/  — mail analyses the analyzer linked to no project
//
// Legacy layout (pre-migration): the 대표페이지 was the flat 프로젝트/<name>.md and
// mail analyses lived under 프로젝트/mail-analyses/[<project>/]. Helpers accept
// both forms during the transition; cmd/wiki-restructure migrates the data.
package wiki

import (
	"path/filepath"
	"strings"
)

// Fixed document slots inside a project folder.
const (
	// RepPageFile is the 대표페이지 filename inside a project folder.
	RepPageFile = "대표.md"
	// LogPageFile is the progress-log filename inside a project folder.
	LogPageFile = "로그.md"
	// EquipmentDir is the per-project equipment/material sub-folder.
	EquipmentDir = "기자재"
	// MailAnalysisDir is the per-project mail-analysis sub-folder, and also the
	// category-level bucket (프로젝트/메일분석/) for analyses with no linked project.
	MailAnalysisDir = "메일분석"
	// legacyMailAnalysisDir is the pre-migration global mail-analysis bucket.
	legacyMailAnalysisDir = "mail-analyses"
	// dealDir is the category-level per-counterparty deal ledger.
	dealDir = "거래"
)

// reservedProjectDirs are direct children of 프로젝트/ that are NOT project
// folders — category-level raw-data buckets a project name must never shadow.
var reservedProjectDirs = map[string]bool{
	dealDir:               true,
	MailAnalysisDir:       true,
	legacyMailAnalysisDir: true,
}

// IsReservedProjectDir reports whether name is a category-level raw-data bucket
// under 프로젝트/ (거래, 메일분석, legacy mail-analyses) rather than a project folder.
func IsReservedProjectDir(name string) bool { return reservedProjectDirs[name] }

// splitProjectPath breaks a slash-normalized wiki path under 프로젝트/ into its
// path segments after the category ("프로젝트/a/b.md" → ["a","b.md"]). Returns nil
// when the path is not under the 프로젝트 category.
func splitProjectPath(relPath string) []string {
	p := filepath.ToSlash(strings.TrimSpace(relPath))
	rest, ok := strings.CutPrefix(p, projectCategoryPrefix+"/")
	if !ok || rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

// RepPagePath returns the 대표페이지 path for a project name:
// "프로젝트/<name>/대표.md".
func RepPagePath(project string) string {
	return projectCategoryPrefix + "/" + project + "/" + RepPageFile
}

// LogPagePath returns the progress-log path for a project name:
// "프로젝트/<name>/로그.md".
func LogPagePath(project string) string {
	return projectCategoryPrefix + "/" + project + "/" + LogPageFile
}

// MailAnalysisPagePath maps a Gmail message ID to its wiki page path: under the
// project's 메일분석/ folder when the analyzer linked one, else the category-level
// unlinked bucket 프로젝트/메일분석/.
func MailAnalysisPagePath(project, msgID string) string {
	if project == "" {
		return projectCategoryPrefix + "/" + MailAnalysisDir + "/" + msgID + ".md"
	}
	return projectCategoryPrefix + "/" + project + "/" + MailAnalysisDir + "/" + msgID + ".md"
}

// IsProjectRepPage reports whether relPath is a project 대표페이지 — the new
// in-folder form 프로젝트/<name>/대표.md, or the legacy flat form 프로젝트/<name>.md
// (accepted during the migration transition).
func IsProjectRepPage(relPath string) bool {
	seg := splitProjectPath(relPath)
	switch len(seg) {
	case 1: // legacy flat 대표페이지: 프로젝트/<name>.md (reserved names are buckets, not projects)
		name := strings.TrimSuffix(seg[0], ".md")
		return strings.HasSuffix(seg[0], ".md") && name != "" && !IsReservedProjectDir(name)
	case 2:
		return !IsReservedProjectDir(seg[0]) && seg[1] == RepPageFile
	default:
		return false
	}
}

// ProjectNameOf extracts the owning project name from any path under a project
// folder (프로젝트/<name>/... or the legacy flat 프로젝트/<name>.md). Returns
// ("", false) for non-project paths and the reserved raw-data buckets.
func ProjectNameOf(relPath string) (string, bool) {
	seg := splitProjectPath(relPath)
	switch {
	case len(seg) == 1 && strings.HasSuffix(seg[0], ".md"): // legacy flat 대표페이지
		name := strings.TrimSuffix(seg[0], ".md")
		if name == "" || IsReservedProjectDir(name) {
			return "", false
		}
		return name, true
	case len(seg) >= 2:
		if IsReservedProjectDir(seg[0]) {
			return "", false
		}
		return seg[0], true
	default:
		return "", false
	}
}

// ProjectFolderOf returns the project folder ("프로젝트/<name>") owning relPath,
// so pages in nested slots (메일분석/, 기자재/) resolve to the same folder as the
// 대표페이지. Returns ("", false) for non-project paths and reserved buckets.
func ProjectFolderOf(relPath string) (string, bool) {
	name, ok := ProjectNameOf(relPath)
	if !ok {
		return "", false
	}
	return projectCategoryPrefix + "/" + name, true
}

// IsProjectRawDataPath reports whether relPath is raw data under 프로젝트/ — a
// mail-analysis page (per-project 메일분석/, category-level 메일분석/, legacy
// mail-analyses/) or a 거래 ledger page — as opposed to curated project content.
func IsProjectRawDataPath(relPath string) bool {
	seg := splitProjectPath(relPath)
	if len(seg) < 2 {
		return false
	}
	if IsReservedProjectDir(seg[0]) {
		return true
	}
	// Per-project raw slots: 프로젝트/<name>/메일분석/... (and legacy nesting).
	return seg[1] == MailAnalysisDir || seg[1] == legacyMailAnalysisDir
}

// NormalizeProjectPagePath rewrites a flat project page path onto the in-folder
// 대표페이지 slot: "프로젝트/<name>.md" → "프로젝트/<name>/대표.md". Every other path
// (nested paths, reserved buckets, other categories) is returned unchanged. This
// keeps the post-migration invariant — no flat pages under 프로젝트/ — for new
// writes from the dreamer and the wiki tool.
func NormalizeProjectPagePath(relPath string) string {
	name, ok := ProjectNameOf(relPath)
	if !ok {
		return relPath
	}
	seg := splitProjectPath(relPath)
	if len(seg) != 1 { // already in a folder
		return relPath
	}
	return RepPagePath(name)
}

// IsMailAnalysisPath reports whether relPath sits in any mail-analysis bucket
// (per-project 메일분석/, category-level 메일분석/, or legacy mail-analyses/) in any
// category. Path-shape only — no page read.
func IsMailAnalysisPath(relPath string) bool {
	p := "/" + filepath.ToSlash(strings.TrimSpace(relPath))
	return strings.Contains(p, "/"+MailAnalysisDir+"/") ||
		strings.Contains(p, "/"+legacyMailAnalysisDir+"/")
}
