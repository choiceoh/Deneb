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
//     (setProjectStatus) — see project_digest.go.
//   - mail analysis (event-driven, no LLM): prepends one dated bullet per
//     project-linked mail (AppendProjectStatusLine) — see the server's mail sink.
//
// The section is a plain newest-first bullet list. Mail appends prepend; the
// dream cycle compacts. A bounded cap keeps it from growing unbounded between
// dream cycles.
package wiki

import (
	"fmt"
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
	Name     string   // display name (page Title, else the project folder name)
	Path     string   // 대표페이지 path, e.g. "프로젝트/영산고/대표.md" (legacy: "프로젝트/영산고.md")
	Summary  string   // page Meta.Summary — one-line description for pickers
	Code     string   // page Meta.Code — frozen project identity, "" if unset
	Client   string   // page Meta.Client — 거래처 (top grouping level + matching key), "" if unset
	Sites    []string // page Meta.Sites — canonical 현장 admin paths (matching keys)
	Kinds    []string // page Meta.Kinds — 에너지원/특성 two-level enum (태양광/루프탑 …, 복수)
	Capacity float64  // page Meta.Capacity — 용량 in MW, 0 if unrecorded
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
			if page.Meta.Archived {
				continue // 종결된 프로젝트 — 활성 무대(후보·모아보기·리서치·리뷰어)에서 제외
			}
			if t := strings.TrimSpace(page.Meta.Title); t != "" {
				ref.Name = t
			}
			ref.Summary = strings.TrimSpace(page.Meta.Summary)
			ref.Code = strings.TrimSpace(page.Meta.Code)
			ref.Client = strings.TrimSpace(page.Meta.Client)
			ref.Sites = page.Meta.Sites
			ref.Kinds = page.Meta.Kinds
			ref.Capacity = page.Meta.Capacity
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
	Client    string   // page Meta.Client — 거래처, the top grouping level; "" if unset
	Refs      []string // graph-resolved owned page paths (code-shared sub-pages + explicitly-linked pages); see projectOwnedRefs
	Summary   string   // page Meta.Summary — the stable one-line description
	Due       string   // page Meta.Due — imminent deadline, "" if none
	Bullets   []string // the "## 현재 상태" lines, newest first
	UpdatedMs int64    // page Meta.Updated (YYYY-MM-DD) as epoch millis, 0 if unparseable
}

// ProjectSite is one 현장 for the 현장 지도 — a single site, not a whole project.
// It comes either from a first-class 현장 page (프로젝트/<name>/현장/<site>.md, with
// its own address·status·용량·에너지원/특성) or, for projects not yet migrated to
// 현장 pages, synthesized from the 대표페이지's flat Meta.Sites (status "" = 미분류).
// Either way one ProjectSite = one pin; Sites holds exactly that site's address.
type ProjectSite struct {
	Name     string   // owning project's display name (the pin's 프로젝트 label)
	Client   string   // 거래처 (site page's, else the project's), "" if unset
	Path     string   // wiki path a tap opens — the 현장 page when there is one, else the 대표페이지
	Due      string   // Meta.Due — imminent deadline, "" if none
	Sites    []string // exactly one canonical 현장 address ("광역약칭 시/군 읍/면")
	Kinds    []string // Meta.Kinds — 에너지원/특성 (태양광/루프탑 …); map colors by 에너지원, shapes by 특성
	Capacity float64  // Meta.Capacity — this site's 용량 in MW; the map sizes pins by this
	Status   string   // 현장 page's lifecycle (후보/계약/개설/준공); "" = 미분류 (fallback rows)
	// 공정 일정 — the milestone dates the 현장 지도 renders as a timeline (blank for
	// 대표페이지-fallback rows, which have no per-site schedule). YYYY-MM-DD, except
	// ModuleDelivery which may be a free-form 기간 (e.g. "3월 중순~4월 초").
	ContractDate         string // 계약일
	ConstructionStart    string // 공사개시일
	ModuleDelivery       string // 모듈입고(기간 가능)
	PreUseInspection     string // 사용전검사일
	CompletionInspection string // 준공검사일
}

// ProjectSites enumerates every 현장 across all active projects for the 현장 지도.
// For each active project it emits one row per first-class 현장 page (rich, with
// per-site status·용량), then falls back to the 대표페이지's flat Meta.Sites for any
// address not yet covered by a 현장 page — so a project keeps showing every site
// during the migration to 현장 pages, gaining per-site fields as pages are created.
// Sorted by project name then address.
func (s *Store) ProjectSites() ([]ProjectSite, error) {
	refs := s.knownProjects()
	// One corpus pass to bucket 현장 pages under their owning project's FOLDER name
	// (프로젝트/<folder>/현장/…). knownProjects overwrites ref.Name with the page
	// title, which often differs from the folder slug, so we must key + look up by
	// folder name (ProjectNameOf), not by ref.Name — else titled projects lose all
	// their 현장 pages.
	sitePagesByProject := make(map[string][]string)
	if paths, err := s.ListPages(projectCategoryPrefix); err == nil {
		for _, p := range paths {
			if !IsProjectSitePage(p) {
				continue
			}
			if folder, ok := ProjectNameOf(p); ok {
				sitePagesByProject[folder] = append(sitePagesByProject[folder], p)
			}
		}
	}

	out := make([]ProjectSite, 0, len(refs))
	for i := range refs {
		ref := &refs[i]
		folder, _ := ProjectNameOf(ref.Path) // the project's folder slug (== ref.Name only when untitled)
		covered := make(map[string]bool)
		for _, sp := range sitePagesByProject[folder] {
			page, err := s.ReadPage(sp)
			if err != nil || page == nil {
				continue
			}
			addr := normalizeSiteName(page.Meta.Address)
			client := strings.TrimSpace(page.Meta.Client)
			if client == "" {
				client = ref.Client
			}
			out = append(out, ProjectSite{
				Name:                 ref.Name,
				Client:               client,
				Path:                 sp,
				Due:                  strings.TrimSpace(page.Meta.Due),
				Sites:                addrSlice(addr),
				Kinds:                page.Meta.Kinds,
				Capacity:             page.Meta.Capacity,
				Status:               strings.TrimSpace(page.Meta.Status),
				ContractDate:         strings.TrimSpace(page.Meta.ContractDate),
				ConstructionStart:    strings.TrimSpace(page.Meta.ConstructionStart),
				ModuleDelivery:       strings.TrimSpace(page.Meta.ModuleDelivery),
				PreUseInspection:     strings.TrimSpace(page.Meta.PreUseInspection),
				CompletionInspection: strings.TrimSpace(page.Meta.CompletionInspection),
			})
			if addr != "" {
				covered[addr] = true
			}
		}
		// Fallback: 대표페이지 addresses without a 현장 page — one status-blank pin each.
		if len(ref.Sites) == 0 {
			continue
		}
		var repDue string
		if page, err := s.ReadPage(ref.Path); err == nil && page != nil {
			repDue = strings.TrimSpace(page.Meta.Due)
		}
		for _, addr := range ref.Sites {
			na := normalizeSiteName(addr)
			if na == "" || covered[na] {
				continue
			}
			covered[na] = true
			out = append(out, ProjectSite{
				Name:     ref.Name,
				Client:   ref.Client,
				Path:     ref.Path,
				Due:      repDue,
				Sites:    []string{addr},
				Kinds:    ref.Kinds,
				Capacity: ref.Capacity,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return firstOf(out[i].Sites) < firstOf(out[j].Sites)
	})
	return out, nil
}

// addrSlice returns [addr] for a non-empty address, else an empty slice. A 현장
// page with no address yet still yields a row (with its status/용량) — the clients
// route an empty-sites row into the 미배치 tray, so it stays visible instead of
// silently vanishing while it's being authored.
func addrSlice(addr string) []string {
	if addr == "" {
		return []string{}
	}
	return []string{addr}
}

func firstOf(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
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
			Client:    strings.TrimSpace(page.Meta.Client),
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

// setProjectStatus replaces a project page's 현재 상태 section with a fresh
// roll-up (the dream cycle's compacted lines, most salient first). Creates the
// page if absent. now stamps Updated (injected for deterministic tests).
func (s *Store) setProjectStatus(relPath string, lines []string, due string, now time.Time) error {
	// Dedupe while cleaning: the 2026-07 audit found rep pages whose whole
	// 현재 상태 was the same no-information bullet twice ("테스트베드 구축
	// 진행" ×2) — a duplicate line adds zero signal at any position.
	seen := make(map[string]bool, len(lines))
	clean := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln = strings.TrimSpace(ln); ln != "" && !seen[ln] {
			seen[ln] = true
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

// AppendProjectStatusLine prepends one capture-dated bullet to a project page's
// 현재 상태 (the event-driven mail/meeting path). Idempotent by ref: a line
// already recorded for that ref is a no-op (keeps Updated stable). Creates the
// page if absent.
func (s *Store) AppendProjectStatusLine(relPath, line, ref string, now time.Time) error {
	return s.AppendProjectStatusLineAt(relPath, line, "", ref, now)
}

// AppendProjectStatusLineAt is AppendProjectStatusLine with an optional event
// date (YYYY-MM-DD) — "when it happened" (e.g. a 견적서's document date), distinct
// from now ("when captured"). When the event date is a valid date on a DIFFERENT
// day than capture, the bullet leads with the event date and notes the capture
// day ("… (8/3 기록)") so the reader isn't misled into reading the processing day
// as the event day; an empty/invalid/same-day event date renders capture-dated as
// before.
func (s *Store) AppendProjectStatusLineAt(relPath, line, eventDate, ref string, now time.Time) error {
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
		lead, captureNote := statusBulletDates(eventDate, now)
		bullet := "- " + lead + " " + line + captureNote + marker
		page.Body = prependStatusBullet(page.Body, bullet)
		page.Meta.Updated = now.Format("2006-01-02")
		return page, nil
	})
}

// statusBulletDates renders a status bullet's leading date and optional capture
// note. With a valid event date on a different day than capture, lead is the
// event date ("1월 2일") and captureNote is " (1/2 기록)"; otherwise lead is the
// capture date and captureNote is empty (dual-timestamp only when it adds info).
func statusBulletDates(eventDate string, now time.Time) (lead, captureNote string) {
	ev, ok := parseStatusEventDate(eventDate)
	if !ok || ev.Format("2006-01-02") == now.Format("2006-01-02") {
		return now.Format("1월 2일"), ""
	}
	return ev.Format("1월 2일"), fmt.Sprintf(" (%d/%d 기록)", int(now.Month()), now.Day())
}

// parseStatusEventDate reads a leading YYYY-MM-DD from s (the deal extractor's
// primary date format; trailing text is tolerated). Returns ok=false for empty
// or non-date input so the caller falls back to the capture date.
func parseStatusEventDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
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
