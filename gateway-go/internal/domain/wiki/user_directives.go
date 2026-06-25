// user_directives.go — procedural memory for the MAIN agent.
//
// The dreamer already distills durable user preferences/corrections into wiki
// pages under the 사용자 (user) category (선호·톤 규칙·개인 컨텍스트). But those
// pages were only ever *recalled* (surfaced as evidence when a cue matched), so
// a standing directive like "always answer in concise Korean" applied only when
// recall happened to fetch it — semantic memory, not procedural.
//
// This pass promotes the active (non-superseded) 사용자 pages into a managed
// "## 행동 지침" section of the workspace USER.md, which the system prompt loads
// every turn (prompt/context_files.go). So the preference becomes *applied*
// behavior, not maybe-recalled fact — LangMem's procedural-memory idea, fitted
// to Deneb (no new model call: the dreamer's existing 사용자 synthesis is the
// distiller; the wiki's supersede flow is the consolidation).
//
// Cache safety: USER.md is a context file, session-frozen like MEMORY.md (which
// the dreamer already rewrites each cycle). The section is byte-stable when the
// 사용자 set is unchanged, and the write is skipped on a byte-identical merge, so
// it never needlessly invalidates the per-session prompt snapshot. Opt-in via
// DENEB_USER_DIRECTIVES=1 so the operator can review the generated USER.md
// before it shapes the agent.
package wiki

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	userFileName     = "USER.md"
	userPrefCategory = "사용자" // == Categories[4]; the user-preferences bucket

	// Marker-delimited, auto-managed block in USER.md. Everything outside the
	// markers is the user's own content and is preserved byte-for-byte.
	userDirectivesBegin   = "<!-- deneb:directives:begin -->"
	userDirectivesEnd     = "<!-- deneb:directives:end -->"
	userDirectivesHeading = "## 행동 지침 (자동 생성 — 이 블록은 직접 편집하지 마세요)"

	// maxUserDirectives caps how many active 사용자 pages surface as directives,
	// keeping USER.md a bounded, glanceable behavior policy.
	maxUserDirectives = 20
)

// userDirectivesEnabled reports whether the procedural-memory pass is on.
func userDirectivesEnabled() bool { return os.Getenv("DENEB_USER_DIRECTIVES") == "1" }

// userDirective is one rendered behavior directive sourced from an active
// 사용자 wiki page.
type userDirective struct {
	title      string
	summary    string
	importance float64
	updated    string // YYYY-MM-DD
}

// distillUserDirectives gathers the active (non-superseded, non-archived) 사용자
// pages, renders them into the managed USER.md section, and writes USER.md only
// when the merged content actually changed. Returns the directive count.
//
// No-op (count 0, nil error) when there is no workspace or store wired.
func (wd *WikiDreamer) distillUserDirectives() (int, error) {
	if wd.workspaceDir == "" || wd.store == nil {
		return 0, nil
	}

	rels, err := wd.store.ListPages(userPrefCategory)
	if err != nil {
		return 0, err
	}
	directives := make([]userDirective, 0, len(rels))
	for _, rel := range rels {
		p, perr := wd.store.ReadPage(rel)
		if perr != nil || p == nil {
			continue
		}
		if p.Meta.SupersededBy != "" || p.Meta.Archived {
			continue // stale facts must not become standing directives
		}
		title := strings.TrimSpace(p.Meta.Title)
		if title == "" {
			title = directiveTitleFromRel(rel)
		}
		directives = append(directives, userDirective{
			title:      title,
			summary:    p.Meta.Summary,
			importance: p.Meta.Importance,
			updated:    p.Meta.Updated,
		})
	}

	section := renderUserDirectivesSection(directives)
	path := filepath.Join(wd.workspaceDir, userFileName)
	existing, _ := os.ReadFile(path) // missing file → empty, merge appends
	merged := mergeUserDirectives(string(existing), section)
	if merged == string(existing) {
		return len(directives), nil // byte-stable: no write, no cache churn
	}
	if err := writeFileAtomic(path, []byte(merged)); err != nil {
		return 0, err
	}
	return len(directives), nil
}

// renderUserDirectivesSection renders the managed section body (heading +
// bullets), most-important-then-newest first. Returns "" when empty so the
// section collapses away.
func renderUserDirectivesSection(ds []userDirective) string {
	if len(ds) == 0 {
		return ""
	}
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].importance != ds[j].importance {
			return ds[i].importance > ds[j].importance
		}
		if ds[i].updated != ds[j].updated {
			return ds[i].updated > ds[j].updated
		}
		return ds[i].title < ds[j].title
	})
	if len(ds) > maxUserDirectives {
		ds = ds[:maxUserDirectives]
	}

	var b strings.Builder
	b.WriteString(userDirectivesHeading)
	b.WriteByte('\n')
	for _, d := range ds {
		line := strings.TrimSpace(d.title)
		if s := strings.TrimSpace(d.summary); s != "" {
			line += ": " + s
		}
		b.WriteString("- ")
		b.WriteString(collapseSpaces(line))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergeUserDirectives replaces (or inserts, or removes) the marker-delimited
// directives block in existing USER.md with the rendered section, preserving
// all other content byte-for-byte. The operation is idempotent: re-merging the
// same section yields byte-identical output, so an unchanged 사용자 set never
// rewrites USER.md.
func mergeUserDirectives(existing, section string) string {
	block := wrapDirectives(section)
	bi := strings.Index(existing, userDirectivesBegin)
	ei := strings.Index(existing, userDirectivesEnd)

	if bi >= 0 && ei > bi {
		before := existing[:bi]
		after := existing[ei+len(userDirectivesEnd):]
		if block == "" {
			// Remove the block; collapse the surrounding blank lines so repeated
			// empty merges stay stable.
			lead := strings.TrimRight(before, "\n")
			tail := strings.TrimLeft(after, "\n")
			switch {
			case lead == "":
				return tail
			case tail == "":
				return lead
			default:
				return lead + "\n\n" + tail
			}
		}
		return before + block + after
	}

	if block == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return block + "\n"
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n"
}

// wrapDirectives wraps a non-empty section body in the begin/end markers.
func wrapDirectives(section string) string {
	if section == "" {
		return ""
	}
	return userDirectivesBegin + "\n" + section + "\n" + userDirectivesEnd
}

// collapseSpaces flattens any run of whitespace (incl. newlines) to single
// spaces so a multi-line summary renders as one bullet line.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// directiveTitleFromRel derives a fallback title from a page's relative path.
func directiveTitleFromRel(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), ".md")
}

// writeFileAtomic writes data to path via a temp file + rename so a reader
// (the prompt's context-file loader) never sees a torn write.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306 — workspace file, wired at startup
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
