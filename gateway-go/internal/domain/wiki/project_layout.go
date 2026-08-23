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
//	  자료/*.md    — ingested external sources (URL/영상), one page per source URL
//	  회의록/*.md  — meeting records (one page per Plaud recording analysis)
//
// Category-level (non-project) buckets under 프로젝트/:
//
//	프로젝트/거래/      — per-counterparty deal ledger pages (span projects)
//	프로젝트/메일분석/  — mail analyses the analyzer linked to no project
//	프로젝트/자료/      — ingested sources linked to no project
//	프로젝트/회의록/    — meeting records linked to no project
//
// Legacy layout (pre-migration): the 대표페이지 was the flat 프로젝트/<name>.md and
// mail analyses lived under 프로젝트/mail-analyses/[<project>/]. Helpers accept
// both forms during the transition; cmd/wiki-restructure migrates the data.
package wiki

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Fixed document slots inside a project folder.
const (
	// RepPageFile is the 대표페이지 filename inside a project folder.
	RepPageFile = "대표.md"
	// LogPageFile is the progress-log filename inside a project folder.
	LogPageFile = "로그.md"
	// equipmentDir is the per-project equipment/material sub-folder.
	equipmentDir = "기자재"
	// mailAnalysisDir is the per-project mail-analysis sub-folder, and also the
	// category-level bucket (프로젝트/메일분석/) for analyses with no linked project.
	mailAnalysisDir = "메일분석"
	// materialDir is the per-project ingested-source sub-folder (URL/영상 자료,
	// one page per source URL — wiki tool action="ingest"), and also the
	// category-level bucket (프로젝트/자료/) for sources with no linked project.
	materialDir = "자료"
	// meetingDir is the per-project meeting-record sub-folder (one page per
	// Plaud recording — plaud_recordings.go), and also the category-level
	// bucket (프로젝트/회의록/) for meetings linked to no project.
	meetingDir = "회의록"
	// siteDir is the per-project 현장 sub-folder (one page per site — its
	// address, 계약/개설 status, per-site 용량·에너지원/특성). The 현장 지도 reads
	// these as first-class site entities. Per-project only (no category-level
	// bucket — a site always belongs to a project), like 기자재.
	siteDir = "현장"
	// legacyMailAnalysisDir is the pre-migration global mail-analysis bucket.
	legacyMailAnalysisDir = "mail-analyses"
	// dealDir is the category-level per-counterparty deal ledger.
	dealDir = "거래"
)

// reservedProjectDirs are direct children of 프로젝트/ that are NOT project
// folders — category-level raw-data buckets a project name must never shadow.
var reservedProjectDirs = map[string]bool{
	dealDir:               true,
	mailAnalysisDir:       true,
	legacyMailAnalysisDir: true,
	materialDir:           true,
	meetingDir:            true,
}

// isReservedProjectDir reports whether name is a category-level raw-data bucket
// under 프로젝트/ (거래, 메일분석, legacy mail-analyses) rather than a project folder.
func isReservedProjectDir(name string) bool { return reservedProjectDirs[name] }

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

// SitePagePath returns a 현장 page path for a project + site name:
// "프로젝트/<project>/현장/<site>.md". The site name is the page's identity (a
// human-readable 현장명 like "수산리" or the address tail), NOT the full address —
// the canonical address lives in the page's frontmatter.
func SitePagePath(project, site string) string {
	return projectCategoryPrefix + "/" + project + "/" + siteDir + "/" + site + ".md"
}

// IsProjectSitePage reports whether relPath is a per-project 현장 page
// (프로젝트/<name>/현장/<site>.md). Path-shape only — no page read.
func IsProjectSitePage(relPath string) bool {
	seg := splitProjectPath(relPath)
	return len(seg) == 3 && !isReservedProjectDir(seg[0]) && seg[1] == siteDir && strings.HasSuffix(seg[2], ".md")
}

// MailAnalysisPagePath maps a Gmail message ID to its wiki page path: under the
// project's 메일분석/ folder when the analyzer linked one, else the category-level
// unlinked bucket 프로젝트/메일분석/.
func MailAnalysisPagePath(project, msgID string) string {
	if project == "" {
		return projectCategoryPrefix + "/" + mailAnalysisDir + "/" + msgID + ".md"
	}
	return projectCategoryPrefix + "/" + project + "/" + mailAnalysisDir + "/" + msgID + ".md"
}

// MaterialPagePath maps an ingested-source filename to its wiki page path:
// under the project's 자료/ folder when the ingest linked one, else the
// category-level unlinked bucket 프로젝트/자료/.
func MaterialPagePath(project, filename string) string {
	if project == "" {
		return projectCategoryPrefix + "/" + materialDir + "/" + filename
	}
	return projectCategoryPrefix + "/" + project + "/" + materialDir + "/" + filename
}

// MeetingPagePath maps a meeting-record filename to its wiki page path: under
// the project's 회의록/ folder when the analyzer linked one, else the
// category-level unlinked bucket 프로젝트/회의록/.
func MeetingPagePath(project, filename string) string {
	if project == "" {
		return projectCategoryPrefix + "/" + meetingDir + "/" + filename
	}
	return projectCategoryPrefix + "/" + project + "/" + meetingDir + "/" + filename
}

// projectOfLinkedMailAnalysis returns the owning project of a PROJECT-LINKED
// mail-analysis page ("프로젝트/<project>/메일분석/<msgID>.md") and true, or
// ("", false) for everything else — including the unlinked category bucket
// (프로젝트/메일분석/) and legacy dirs, whose senders have no established
// project relationship and must not count as active counterparties.
func projectOfLinkedMailAnalysis(relPath string) (string, bool) {
	seg := splitProjectPath(relPath)
	if len(seg) != 3 || seg[1] != mailAnalysisDir || !strings.HasSuffix(seg[2], ".md") {
		return "", false
	}
	if seg[0] == "" || isReservedProjectDir(seg[0]) {
		return "", false
	}
	return seg[0], true
}

// IsProjectRepPage reports whether relPath is a project 대표페이지 — the new
// in-folder form 프로젝트/<name>/대표.md, or the legacy flat form 프로젝트/<name>.md
// (accepted during the migration transition).
func IsProjectRepPage(relPath string) bool {
	seg := splitProjectPath(relPath)
	switch len(seg) {
	case 1: // legacy flat 대표페이지: 프로젝트/<name>.md (reserved names are buckets, not projects)
		name := strings.TrimSuffix(seg[0], ".md")
		return strings.HasSuffix(seg[0], ".md") && name != "" && !isReservedProjectDir(name)
	case 2:
		return !isReservedProjectDir(seg[0]) && seg[1] == RepPageFile
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
		if name == "" || isReservedProjectDir(name) {
			return "", false
		}
		return name, true
	case len(seg) >= 2:
		if isReservedProjectDir(seg[0]) {
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
	if isReservedProjectDir(seg[0]) {
		return true
	}
	// Per-project raw slots: 프로젝트/<name>/{메일분석,자료,회의록}/... (and legacy nesting).
	return seg[1] == mailAnalysisDir || seg[1] == legacyMailAnalysisDir || seg[1] == materialDir || seg[1] == meetingDir
}

// IsDealLedgerPath reports whether relPath is a per-counterparty deal ledger
// page (프로젝트/거래/…). forget refuses these: they mirror the financial deal
// records (.deals.jsonl) — 견적·계약·세금계산서 amounts and counts — which are a
// business/audit surface, not a stray fact to erase on request.
func IsDealLedgerPath(relPath string) bool {
	seg := splitProjectPath(relPath)
	return len(seg) >= 1 && seg[0] == dealDir
}

// NormalizeProjectPagePath enforces the project layout's path shape on a write:
//
//   - a flat "프로젝트/<name>.md" routes onto the 대표페이지 slot
//     ("프로젝트/<name>/대표.md") — no flat pages after the migration;
//   - segments deeper than the schema allows fold into the filename. LLM writers
//     emit titles with dates like "재구매 — 6/25 회의", whose slashes would mint
//     phantom nested project folders (a real 2026-07-02 incident: the dreamer
//     created 프로젝트/…-6/25-…/06-….md). The schema bounds depth — 프로젝트/<name>/
//     <file> or 프로젝트/<name>/<기자재|메일분석>/<file> — so anything deeper is a
//     malformed filename, not an intended hierarchy.
//
// Reserved buckets (거래/, 메일분석/, legacy mail-analyses/) and other categories
// are returned unchanged.
func NormalizeProjectPagePath(relPath string) string {
	name, ok := ProjectNameOf(relPath)
	if !ok {
		return relPath
	}
	seg := splitProjectPath(relPath)
	switch {
	case len(seg) == 1: // legacy flat 대표페이지 form
		return RepPagePath(name)
	case len(seg) == 2: // 프로젝트/<name>/<file>.md — canonical detail/slot
		return relPath
	case seg[1] == equipmentDir || seg[1] == mailAnalysisDir || seg[1] == legacyMailAnalysisDir || seg[1] == materialDir || seg[1] == meetingDir || seg[1] == siteDir:
		if len(seg) == 3 { // canonical slot file
			return relPath
		}
		// Overdeep under a slot dir: fold the tail into one filename.
		return projectCategoryPrefix + "/" + name + "/" + seg[1] + "/" + strings.Join(seg[2:], "-")
	default:
		// Overdeep under the project folder: the "sub-folders" are slash debris
		// from a title — fold everything after the project into one filename.
		return projectCategoryPrefix + "/" + name + "/" + strings.Join(seg[1:], "-")
	}
}

// mailSuffixTokens are trailing name segments that mark a MAIL SUBJECT, not a
// project identity ("…-가배치-요청-(2026-06-25)"). Stripped at mint time only.
var mailSuffixTokens = map[string]bool{
	"요청": true, "송부": true, "의견": true, "문의": true, "회신": true,
	"안내": true, "알림": true, "공지": true, "전달": true,
}

// trailingDateRe matches a trailing "(YYYY-MM-DD)" stamp (with or without
// parens, with any joining dashes) — the date a mail arrived, not part of the
// project name. Matched on the WHOLE tail because the date's own hyphens would
// shred under segment splitting.
var trailingDateRe = regexp.MustCompile(`[-–—\s]*\(?\d{4}-\d{2}-\d{2}\)?\.?$`)

// cleanProjectFolderName strips mail-subject debris from a project name:
// trailing date stamps, request-suffix tokens, and dangling dashes. The 2026-07
// audit found 5 of 56 live folders named after a mail subject
// ("…-법무검토-의견-(2026-06-30)"). Mint-time only — existing folders are never
// renamed by this (path stability); see CleanNewProjectRepPath.
func cleanProjectFolderName(name string) string {
	out := strings.TrimSpace(name)
	for {
		prev := out
		out = strings.TrimRight(trailingDateRe.ReplaceAllString(out, ""), "-–— ")
		segs := strings.Split(out, "-")
		for len(segs) > 1 {
			last := strings.TrimSpace(segs[len(segs)-1])
			if last == "" || last == "—" || last == "–" || mailSuffixTokens[last] {
				segs = segs[:len(segs)-1]
				continue
			}
			break
		}
		out = strings.Join(segs, "-")
		if out == prev {
			break
		}
	}
	if strings.TrimSpace(out) == "" {
		return name
	}
	return out
}

// CleanNewProjectRepPath applies cleanProjectFolderName when path would mint a
// NEW project rep page: the existing folder keeps its path (stability), and if
// the CLEANED folder already exists the write routes into it instead of
// minting a subject-named twin.
func (s *Store) CleanNewProjectRepPath(path string) string {
	if !IsProjectRepPage(path) {
		return path
	}
	if _, err := s.ReadPage(path); err == nil {
		return path // already exists — never rename in-place
	}
	name, ok := ProjectNameOf(path)
	if !ok {
		return path
	}
	clean := cleanProjectFolderName(name)
	if clean == name {
		return path
	}
	return RepPagePath(clean)
}

// IsMaterialPath reports whether relPath sits in any ingested-source bucket
// (per-project 자료/ or the category-level 프로젝트/자료/). Path-shape only.
func IsMaterialPath(relPath string) bool {
	p := "/" + filepath.ToSlash(strings.TrimSpace(relPath))
	return strings.Contains(p, "/"+materialDir+"/")
}

// IsMailAnalysisPath reports whether relPath sits in any mail-analysis bucket
// (per-project 메일분석/, category-level 메일분석/, or legacy mail-analyses/) in any
// category. Path-shape only — no page read.
func IsMailAnalysisPath(relPath string) bool {
	p := "/" + filepath.ToSlash(strings.TrimSpace(relPath))
	return strings.Contains(p, "/"+mailAnalysisDir+"/") ||
		strings.Contains(p, "/"+legacyMailAnalysisDir+"/")
}

// IsUnlinkedMailAnalysisPath reports whether relPath is a mail analysis that
// was never filed under a project folder — the category-level staging bucket
// 프로젝트/메일분석/<id>.md (and the legacy mail-analyses/ equivalent). Filed
// analyses (프로젝트/<name>/메일분석/…) return false.
func IsUnlinkedMailAnalysisPath(relPath string) bool {
	if !IsMailAnalysisPath(relPath) {
		return false
	}
	_, ok := ProjectNameOf(relPath)
	return !ok
}

// mailAnalysisMsgID returns a mail-analysis page's identifying Gmail message ID,
// which the mail sink encodes as the filename stem (메일 1통 = 1페이지 — see
// MailAnalysisPagePath). Empty for non-mail pages. Two mail-analysis pages are
// the SAME mail iff this matches: a shared subject line does NOT make them one
// (reply chains, re-sends, and vendor notifications routinely repeat a subject),
// so subject-similarity dedup must never fold two differing IDs together.
func mailAnalysisMsgID(relPath string) string {
	if !IsMailAnalysisPath(relPath) {
		return ""
	}
	p := filepath.ToSlash(strings.TrimSpace(relPath))
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".md")
}
