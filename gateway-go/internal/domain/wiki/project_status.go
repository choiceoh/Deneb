// project_status.go — the project representative page's "## 현재 상태" section.
//
// A project lives as a single page 프로젝트/<name>.md (the same direct-page
// convention the mail analyzer's related-project candidates use; the nested
// 프로젝트/mail-analyses/ and 프로젝트/거래/ folders are raw data, not projects).
// That page is the project's 대표페이지, and its "## 현재 상태" section is the
// living latest-progress digest the 모아보기 screen reads.
//
// Two writers keep it fresh:
//   - the dream cycle (periodic, LLM): replaces the section with a clean roll-up
//     (SetProjectStatus) — see project_digest.go.
//   - mail analysis (event-driven, no LLM): prepends one dated bullet per
//     project-linked mail (AppendProjectStatusLine) — see the server's mail sink.
//
// The section is a plain newest-first bullet list. Mail appends prepend; the
// dream cycle compacts. A bounded cap keeps it from growing unbounded between
// dream cycles.
package wiki

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// projectStatusHeading is the H2 section on a project page holding its latest
// progress. parseable by SplitByH2.
const projectStatusHeading = "현재 상태"

// maxProjectStatusBullets caps the section so event-driven mail appends can't
// grow it without bound between dream-cycle compactions.
const maxProjectStatusBullets = 8

// ProjectRef names a real project bucket via its 대표페이지.
type ProjectRef struct {
	Name    string // display name (page Title, else the project folder name)
	Path    string // 대표페이지 path, e.g. "프로젝트/영산고/대표.md" (legacy: "프로젝트/영산고.md")
	Summary string // page Meta.Summary — one-line description for pickers
}

// KnownProjects lists the real projects by their 대표페이지 (see project_layout.go;
// legacy flat pages count during the migration transition). Sorted by name. This
// is the anchor set for digests and the mail analyzer's related-project
// candidates: a project label that isn't here can't be navigated to, so it's
// never persisted.
func (s *Store) KnownProjects() []ProjectRef { return s.knownProjects() }

func (s *Store) knownProjects() []ProjectRef {
	paths, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return nil
	}
	// Collect rep pages keyed by project name; when a project has both the
	// in-folder 대표.md and a leftover legacy flat page, the folder form wins.
	repByName := make(map[string]string, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(p)
		if !IsProjectRepPage(p) {
			continue
		}
		name, ok := ProjectNameOf(p)
		if !ok {
			continue
		}
		if prev, dup := repByName[name]; dup {
			if strings.HasSuffix(prev, "/"+RepPageFile) {
				continue // keep the in-folder form
			}
		}
		repByName[name] = p
	}
	refs := make([]ProjectRef, 0, len(repByName))
	for name, p := range repByName {
		ref := ProjectRef{Name: name, Path: p}
		if page, perr := s.ReadPage(p); perr == nil && page != nil {
			if t := strings.TrimSpace(page.Meta.Title); t != "" {
				ref.Name = t
			}
			ref.Summary = strings.TrimSpace(page.Meta.Summary)
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// ProjectStatus is one project's current digest, parsed from its 대표페이지.
type ProjectStatus struct {
	Name      string
	Path      string
	Code      string   // page Meta.Code — frozen composite project identity, "" if unset
	Refs      []string // graph-resolved owned page paths (code-shared sub-pages + explicitly-linked pages); see projectOwnedRefs
	Summary   string   // page Meta.Summary — the stable one-line description
	Due       string   // page Meta.Due — imminent deadline, "" if none
	Bullets   []string // the "## 현재 상태" lines, newest first
	UpdatedMs int64    // page Meta.Updated (YYYY-MM-DD) as epoch millis, 0 if unparseable
}

// ProjectStatuses returns each project that has a non-empty 현재 상태 section,
// newest-updated first. Projects with no status yet are omitted (the 모아보기
// shows only what has actually moved). Satisfies the miniapp.project.digests
// read path.
func (s *Store) ProjectStatuses() ([]ProjectStatus, error) {
	refs := s.knownProjects()
	out := make([]ProjectStatus, 0, len(refs))
	for _, ref := range refs {
		page, err := s.ReadPage(ref.Path)
		if err != nil || page == nil {
			continue
		}
		bullets := extractStatusBullets(page.Body)
		if len(bullets) == 0 {
			continue
		}
		out = append(out, ProjectStatus{
			Name:      ref.Name,
			Path:      ref.Path,
			Code:      strings.TrimSpace(page.Meta.Code),
			Summary:   strings.TrimSpace(page.Meta.Summary),
			Due:       strings.TrimSpace(page.Meta.Due),
			Bullets:   bullets,
			UpdatedMs: dateToMillis(page.Meta.Updated),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedMs != out[j].UpdatedMs {
			return out[i].UpdatedMs > out[j].UpdatedMs // newest first
		}
		return out[i].Name < out[j].Name
	})
	// Resolve each project's owned pages from the live wiki graph (one corpus
	// pass) so the client can link items that reference a sub/deal page, not just
	// the 대표페이지 itself. Not a hot path — the digest is cached client-side.
	owned := s.projectOwnedRefs(out)
	for i := range out {
		out[i].Refs = owned[out[i].Path]
	}
	return out, nil
}

// SetProjectStatus replaces a project page's 현재 상태 section with a fresh
// roll-up (the dream cycle's compacted lines, most salient first). Creates the
// page if absent. now stamps Updated (injected for deterministic tests).
func (s *Store) SetProjectStatus(relPath string, lines []string, due string, now time.Time) error {
	clean := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln = strings.TrimSpace(ln); ln != "" {
			clean = append(clean, ln)
		}
	}
	if len(clean) == 0 {
		return nil // nothing to write; leave any prior status intact
	}
	if len(clean) > maxProjectStatusBullets {
		clean = clean[:maxProjectStatusBullets]
	}
	return s.UpdatePage(relPath, func(existing *Page) (*Page, error) {
		page := ensureProjectPage(existing, relPath)
		page.Body = upsertSection(page.Body, projectStatusHeading, renderBullets(clean))
		page.Meta.Updated = now.Format("2006-01-02")
		if d := strings.TrimSpace(due); d != "" {
			page.Meta.Due = d
		}
		return page, nil
	})
}

// AppendProjectStatusLine prepends one dated bullet to a project page's 현재 상태
// (the event-driven mail path). Idempotent by ref: a line already recorded for
// that ref is a no-op (keeps Updated stable). Creates the page if absent.
func (s *Store) AppendProjectStatusLine(relPath, line, ref string, now time.Time) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	return s.UpdatePage(relPath, func(existing *Page) (*Page, error) {
		page := ensureProjectPage(existing, relPath)
		marker := ""
		if r := strings.TrimSpace(ref); r != "" {
			if strings.Contains(page.Body, dealRefMarker(r)) {
				return nil, nil // already recorded → skip write
			}
			marker = " " + dealRefMarker(r)
		}
		bullet := "- " + now.Format("1월 2일") + " " + line + marker
		page.Body = prependStatusBullet(page.Body, bullet)
		page.Meta.Updated = now.Format("2006-01-02")
		return page, nil
	})
}

// ensureProjectPage returns existing, or a minimal new project page named after
// its project when absent (defensive — the mail/dream paths anchor to pages
// that already exist, but a project the analyzer linked could have been deleted).
func ensureProjectPage(existing *Page, relPath string) *Page {
	if existing != nil {
		return existing
	}
	// 프로젝트/<name>/대표.md must be titled by the project, not "대표".
	name, ok := ProjectNameOf(relPath)
	if !ok {
		name = strings.TrimSuffix(filepath.Base(filepath.ToSlash(relPath)), ".md")
	}
	page := NewPage(name, projectCategoryPrefix, nil)
	page.Meta.Type = "project"
	return page
}

// renderBullets renders lines as a Markdown bullet list.
func renderBullets(lines []string) string {
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(ln))
	}
	return b.String()
}

// prependStatusBullet inserts newBullet at the top of the 현재 상태 bullet list
// and caps the list to maxProjectStatusBullets (dropping the oldest at the
// bottom). Non-bullet content in the section is discarded — the section is a
// bullet list by construction.
func prependStatusBullet(body, newBullet string) string {
	var existing []string
	_, sections := (&Page{Body: body}).SplitByH2()
	for _, sec := range sections {
		if !strings.EqualFold(strings.TrimSpace(sec.Heading), projectStatusHeading) {
			continue
		}
		for _, ln := range strings.Split(sec.Content, "\n") {
			if t := strings.TrimSpace(ln); strings.HasPrefix(t, "- ") {
				existing = append(existing, t)
			}
		}
	}
	all := append([]string{strings.TrimSpace(newBullet)}, existing...)
	if len(all) > maxProjectStatusBullets {
		all = all[:maxProjectStatusBullets]
	}
	return upsertSection(body, projectStatusHeading, strings.Join(all, "\n"))
}

// extractStatusBullets pulls the 현재 상태 section's bullet lines, newest first,
// stripped of the "- " prefix and any trailing provenance marker.
func extractStatusBullets(body string) []string {
	var out []string
	_, sections := (&Page{Body: body}).SplitByH2()
	for _, sec := range sections {
		if !strings.EqualFold(strings.TrimSpace(sec.Heading), projectStatusHeading) {
			continue
		}
		for _, ln := range strings.Split(sec.Content, "\n") {
			t := strings.TrimSpace(ln)
			if !strings.HasPrefix(t, "- ") {
				continue
			}
			t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
			t = stripTrailingMarker(t)
			if t != "" {
				out = append(out, t)
			}
			if len(out) >= maxProjectStatusBullets {
				break
			}
		}
	}
	return out
}

// stripTrailingMarker removes a trailing inline provenance token (`<ref>`) the
// mail path appends for idempotency, so it never shows in the UI.
func stripTrailingMarker(s string) string {
	s = strings.TrimRight(s, " ")
	if strings.HasSuffix(s, "`") {
		if i := strings.LastIndex(s, " `<"); i >= 0 {
			return strings.TrimRight(s[:i], " ")
		}
	}
	return s
}

// dateToMillis parses a YYYY-MM-DD page date to epoch millis (UTC midnight),
// returning 0 when empty or malformed.
func dateToMillis(date string) int64 {
	date = strings.TrimSpace(date)
	if date == "" {
		return 0
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
