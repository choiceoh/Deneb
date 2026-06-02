package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// WeeklyReportOpts configures the weekly business report.
type WeeklyReportOpts struct {
	WikiDir string // wiki root; scans <WikiDir>/프로젝트 for project pages. Empty = no scan.
}

// weeklySoganOrder is the report's department ordering: 기획조정실 1/2/3팀 →
// 계열사(남도에코) → 실장 개인. Pages without a 소관 tag are dropped (those are
// non-business/meta pages, intentionally left untagged in the backfill).
var weeklySoganOrder = []string{"1팀", "2팀", "3팀", "남도에코", "개인"}

// weeklySoganLabel maps a 소관 tag to its report-section heading.
var weeklySoganLabel = map[string]string{
	"1팀":   "사업개발 (1팀)",
	"2팀":   "태양광 발전 — 루프탑·RE100·주차장 (2팀)",
	"3팀":   "모듈 구매·판매 (3팀)",
	"남도에코": "남도에코에너지 (전선·케이블)",
	"개인":   "실장 직속 개인 프로젝트",
}

// weeklyCapacityRe matches a solar capacity figure (e.g. "2.5MW", "100kW급").
var weeklyCapacityRe = regexp.MustCompile(`(\d[0-9,]*\.?\d*)\s?(MWp|kWp|MW|kW|㎿)`)

// weeklyProject is one project in the report.
type weeklyProject struct {
	Sogan       string `json:"sogan"`
	Title       string `json:"title"`
	Capacity    string `json:"capacity,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Updated     string `json:"updated,omitempty"`
	Due         string `json:"due,omitempty"`
	DaysToDue   *int   `json:"days_to_due,omitempty"`
	Timeline    string `json:"timeline_raw,omitempty"`
	NextActions string `json:"next_actions_raw,omitempty"`
	Path        string `json:"path,omitempty"`
	DoneLine    string `json:"done_line,omitempty"`    // 실시 한 줄 (요약 상태절)
	PlannedLine string `json:"planned_line,omitempty"` // 예정 한 줄 (다음 액션)
}

type weeklyGroup struct {
	Sogan    string          `json:"sogan"`
	Label    string          `json:"label"`
	Projects []weeklyProject `json:"projects"`
}

type weeklyEnvelope struct {
	Office      string        `json:"office"`
	Reporter    string        `json:"reporter"`
	WeekDone    string        `json:"week_done"`
	WeekPlanned string        `json:"week_planned"`
	GeneratedAt string        `json:"generated_at"`
	Groups      []weeklyGroup `json:"groups"`
	Issues      []string      `json:"issues,omitempty"` // 현안 (마감 임박/초과 자동 추출)
}

// collectWeekly scans project wiki pages and builds the report envelope:
// active projects grouped by 소관, with composed 실시/예정 one-liners and
// auto-surfaced 현안 (due within 3 days or overdue).
func collectWeekly(opts WeeklyReportOpts, now time.Time) weeklyEnvelope {
	now = now.In(kstLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daysSinceMon := (int(today.Weekday()) + 6) % 7 // Mon=0
	thisMon := today.AddDate(0, 0, -daysSinceMon)
	lastMon := thisMon.AddDate(0, 0, -7)
	lastSun := thisMon.AddDate(0, 0, -1)
	thisSun := thisMon.AddDate(0, 0, 6)

	byGroup := map[string][]weeklyProject{}
	var issues []string
	if opts.WikiDir != "" {
		projDir := filepath.Join(opts.WikiDir, "프로젝트")
		_ = filepath.Walk(projDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
				return nil //nolint:nilerr // skip inaccessible/non-md
			}
			page, perr := wiki.ParsePageFile(path)
			if perr != nil || page.Meta.Archived {
				return nil //nolint:nilerr // unreadable/archived → skip
			}
			sogan := weeklySoganFromTags(page.Meta.Tags)
			if sogan == "" {
				return nil // non-business/meta
			}
			active := false
			if u := weeklyParseDate(page.Meta.Updated, now.Location()); !u.IsZero() &&
				!u.Before(today.AddDate(0, 0, -10)) {
				active = true
			}
			var daysToDue *int
			if d := weeklyParseDate(page.Meta.Due, now.Location()); !d.IsZero() {
				dd := int(d.Sub(today).Hours() / 24)
				daysToDue = &dd
				if dd >= -7 && dd <= 14 {
					active = true
				}
			}
			if !active {
				return nil
			}
			rel, _ := filepath.Rel(opts.WikiDir, path)
			p := weeklyProject{
				Sogan:       sogan,
				Title:       weeklyShortTitle(page.Meta.Title),
				Capacity:    weeklyCapacity(page.Meta.Title + " " + page.Meta.Summary + " " + page.Body),
				Summary:     page.Meta.Summary,
				Updated:     page.Meta.Updated,
				Due:         page.Meta.Due,
				DaysToDue:   daysToDue,
				Timeline:    weeklyExtractSection(page.Body, "타임라인", "진행 상황", "진행상황"),
				NextActions: weeklyExtractSection(page.Body, "다음 액션", "다음액션", "열린 이슈", "열린이슈"),
				Path:        rel,
			}
			p.DoneLine = weeklyDoneLine(p.Summary, p.Timeline)
			p.PlannedLine = weeklyPlannedLine(p.NextActions, p.Summary)
			byGroup[sogan] = append(byGroup[sogan], p)
			// 현안: 마감 임박(≤3일) 또는 초과
			if daysToDue != nil && *daysToDue <= 3 {
				when := fmt.Sprintf("D-%d", *daysToDue)
				if *daysToDue < 0 {
					when = fmt.Sprintf("기한 %d일 초과", -*daysToDue)
				} else if *daysToDue == 0 {
					when = "오늘 마감"
				}
				issues = append(issues, fmt.Sprintf("%s : 마감 %s (%s)", p.Title, when, page.Meta.Due))
			}
			return nil
		})
	}

	groups := make([]weeklyGroup, 0, len(byGroup))
	for _, sg := range weeklySoganOrder {
		items := byGroup[sg]
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Updated > items[j].Updated })
		groups = append(groups, weeklyGroup{Sogan: sg, Label: weeklySoganLabel[sg], Projects: items})
	}

	return weeklyEnvelope{
		Office:      "기획조정실",
		Reporter:    "오선택 실장",
		WeekDone:    fmt.Sprintf("%s~%s", lastMon.Format("06.01.02"), lastSun.Format("06.01.02")),
		WeekPlanned: fmt.Sprintf("%s~%s", thisMon.Format("06.01.02"), thisSun.Format("06.01.02")),
		GeneratedAt: now.Format(time.RFC3339),
		Groups:      groups,
		Issues:      issues,
	}
}

// CollectWeeklyReportData returns the report envelope as JSON (debug / tests / inspection).
func CollectWeeklyReportData(_ context.Context, opts WeeklyReportOpts, now time.Time) (string, error) {
	out, err := json.MarshalIndent(collectWeekly(opts, now), "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal weekly report data: %w", err)
	}
	return string(out), nil
}

// BuildWeeklyReportPDF collects the report, composes the HTML form, and renders
// it to a PDF via headless Chromium. Returns the PDF path on success; on any
// render failure it returns rendered=false with a plain-text fallback (the
// caller delivers text instead). Never errors on render failure — degrades.
func BuildWeeklyReportPDF(ctx context.Context, opts WeeklyReportOpts, now time.Time) (pdfPath, textFallback string, rendered bool) {
	env := collectWeekly(opts, now)
	textFallback = composeWeeklyText(env)
	html, err := composeWeeklyHTML(env)
	if err != nil {
		return "", textFallback, false
	}
	dir := weeklyOutputDir()
	htmlPath := filepath.Join(dir, "deneb-weekly-report.html")
	pdfPath = filepath.Join(dir, "deneb-weekly-report.pdf")
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil { //nolint:gosec // report html, not secret
		return "", textFallback, false
	}
	_ = os.Remove(pdfPath) // clear stale output so a failed render can't ship last week's file
	// Headless Chromium needs two scarce resources on this host; running out of
	// either silently degrades the PDF to text. Guard both before attempting:
	//   1. RAM: on this unified-memory node Chromium can't reserve its address
	//      space when commit headroom is low (vLLM-loaded) — it hangs to timeout.
	//   2. Disk: it writes a font cache / shared memory under the output dir; a
	//      full filesystem makes it abort with ENOSPC before producing a page.
	if weeklyCommitHeadroomMB() < weeklyMinHeadroomMB || weeklyFreeDiskMB(dir) < weeklyMinDiskMB {
		return "", textFallback, false
	}
	if err := weeklyRenderPDF(ctx, htmlPath, pdfPath); err != nil {
		return "", textFallback, false
	}
	return pdfPath, textFallback, true
}

// weeklyMinHeadroomMB is the commit headroom below which a Chromium render is
// not even attempted (it would hang). Empirically render succeeds with multi-GB
// headroom and hangs near ~1GB on the gx10 head node.
const weeklyMinHeadroomMB = 2048

// weeklyMinDiskMB is the free space (on the output filesystem) below which a
// Chromium render is skipped. Chromium needs scratch space for its font cache
// and shared memory; with less it aborts (ENOSPC) instead of producing a PDF.
const weeklyMinDiskMB = 256

// weeklyOutputDir returns a real-disk directory for the report's HTML/PDF and
// for Chromium's own scratch files. os.TempDir() is /tmp, which on the gx10
// head node is a small tmpfs that routinely fills up (vLLM build trees, large
// media) — a full /tmp makes Chromium abort with ENOSPC mid-render and the
// report silently degrades to text every time. Prefer ~/.cache/deneb-visual on
// the NVMe disk; fall back to os.TempDir() only when that can't be created.
func weeklyOutputDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".cache", "deneb-visual")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return os.TempDir()
}

// weeklyFreeDiskMB returns the available space on dir's filesystem in MB.
// Returns a large value if it can't stat, so a render still attempts.
func weeklyFreeDiskMB(dir string) int {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 1 << 20
	}
	return int(st.Bavail * uint64(st.Bsize) / (1024 * 1024)) //nolint:gosec // sizes well within int
}

// weeklyCommitHeadroomMB returns (CommitLimit - Committed_AS) in MB from
// /proc/meminfo. Returns a large value if unreadable so the render still attempts.
func weeklyCommitHeadroomMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 1 << 20
	}
	var limit, committed int64
	for _, ln := range strings.Split(string(data), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "CommitLimit:":
			fmt.Sscanf(f[1], "%d", &limit) //nolint:errcheck // best-effort parse
		case "Committed_AS:":
			fmt.Sscanf(f[1], "%d", &committed) //nolint:errcheck // best-effort parse
		}
	}
	if limit == 0 {
		return 1 << 20
	}
	return int((limit - committed) / 1024)
}

// --- composition ---

func weeklyShortTitle(t string) string {
	// Trim verbose suffixes for the report line; keep the project name.
	t = strings.TrimSpace(t)
	return t
}

// weeklyDoneLine derives the 실시 one-liner: the status clause of the summary
// (text after the first em-dash), falling back to the latest timeline bullet.
func weeklyDoneLine(summary, timeline string) string {
	if summary != "" {
		if i := strings.Index(summary, "—"); i >= 0 && i+len("—") < len(summary) {
			return weeklyClip(strings.TrimSpace(summary[i+len("—"):]), 160)
		}
		return weeklyClip(summary, 160)
	}
	if ln := weeklyLastBullet(timeline); ln != "" {
		return weeklyClip(ln, 160)
	}
	return "진행 중"
}

// weeklyPlannedLine derives the 예정 one-liner: the first next-action bullet.
func weeklyPlannedLine(nextActions, summary string) string {
	if ln := weeklyFirstBullet(nextActions); ln != "" {
		return weeklyClip(ln, 160)
	}
	if summary != "" {
		return "후속 진행"
	}
	return "—"
}

func weeklyFirstBullet(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if c := weeklyCleanBullet(ln); c != "" {
			return c
		}
	}
	return ""
}

func weeklyLastBullet(s string) string {
	last := ""
	for _, ln := range strings.Split(s, "\n") {
		if c := weeklyCleanBullet(ln); c != "" {
			last = c
		}
	}
	return last
}

// weeklyCleanBullet strips markdown bullet/checkbox/indent markers; returns "" for non-bullets.
func weeklyCleanBullet(ln string) string {
	t := strings.TrimSpace(ln)
	if !strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "*") {
		return ""
	}
	t = strings.TrimLeft(t, "-*")
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "[ ]")
	t = strings.TrimPrefix(t, "[x]")
	t = strings.TrimPrefix(t, "[X]")
	t = strings.TrimSpace(t)
	// Drop bold markers.
	t = strings.ReplaceAll(t, "**", "")
	return t
}

func weeklyClip(s string, maxLen int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > maxLen {
		return string(r[:maxLen]) + "…"
	}
	return s
}

// composeWeeklyText renders a plain-text fallback (delivered when PDF render fails).
func composeWeeklyText(env weeklyEnvelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 주간업무보고 — %s\n실시 %s / 예정 %s\n", env.Office, env.WeekDone, env.WeekPlanned)
	for _, g := range env.Groups {
		fmt.Fprintf(&b, "\n▢ %s\n", g.Label)
		for _, p := range g.Projects {
			capTxt := ""
			if p.Capacity != "" {
				capTxt = "(" + p.Capacity + ")"
			}
			fmt.Fprintf(&b, "  • %s%s\n     - 실시: %s\n     - 예정: %s\n", p.Title, capTxt, p.DoneLine, p.PlannedLine)
		}
	}
	if len(env.Issues) > 0 {
		b.WriteString("\n⚠️ 현안\n")
		for _, is := range env.Issues {
			fmt.Fprintf(&b, "  - %s\n", is)
		}
	}
	return b.String()
}

var weeklyHTMLTmpl = template.Must(template.New("weekly").Parse(weeklyHTMLSrc))

func composeWeeklyHTML(env weeklyEnvelope) (string, error) {
	var buf bytes.Buffer
	if err := weeklyHTMLTmpl.Execute(&buf, env); err != nil {
		return "", fmt.Errorf("weekly html template: %w", err)
	}
	return buf.String(), nil
}

// --- render ---

func weeklyRenderPDF(ctx context.Context, htmlPath, pdfPath string) error {
	bin := weeklyFindChromium()
	if bin == "" {
		return fmt.Errorf("chromium not found")
	}
	// Chromium writes its user profile, font cache, shared memory and crash
	// dumps under TMPDIR / the user-data-dir. Keep all of that on the same
	// real-disk dir as the output — defaulting to the /tmp tmpfs (often full on
	// this host) makes it abort with ENOSPC before producing a page.
	workDir := filepath.Dir(pdfPath)
	udd, err := os.MkdirTemp(workDir, "chrome-")
	if err != nil {
		return fmt.Errorf("chromium scratch dir: %w", err)
	}
	defer os.RemoveAll(udd)
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rctx, bin,
		"--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--hide-scrollbars",
		"--no-pdf-header-footer", "--virtual-time-budget=4000",
		"--user-data-dir="+udd, "--crash-dumps-dir="+udd,
		"--print-to-pdf="+pdfPath, "file://"+htmlPath,
	)
	cmd.Env = append(os.Environ(), "TMPDIR="+udd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chromium print-to-pdf: %w (%s)", err, weeklyClip(string(out), 200))
	}
	if fi, err := os.Stat(pdfPath); err != nil || fi.Size() == 0 {
		return fmt.Errorf("no pdf produced")
	}
	return nil
}

// weeklyFindChromium locates a headless Chromium binary. Override with
// DENEB_REPORT_CHROMIUM; default = the Playwright-bundled headless_shell
// (same engine the miniapp screenshot harness uses), then PATH fallbacks.
func weeklyFindChromium() string {
	if p := os.Getenv("DENEB_REPORT_CHROMIUM"); p != "" {
		return p
	}
	if m, _ := filepath.Glob(os.ExpandEnv("$HOME/.cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell")); len(m) > 0 {
		return m[len(m)-1]
	}
	for _, c := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// --- helpers shared with the collector ---

func weeklySoganFromTags(tags []string) string {
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, "소관:") {
			return strings.TrimSpace(strings.TrimPrefix(t, "소관:"))
		}
	}
	return ""
}

func weeklyParseDate(s string, loc *time.Location) time.Time {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		s = s[:10]
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

func weeklyCapacity(s string) string {
	return strings.TrimSpace(weeklyCapacityRe.FindString(s))
}

func weeklyExtractSection(body string, headers ...string) string {
	var out []string
	capturing := false
	for _, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			matched := false
			for _, h := range headers {
				if strings.Contains(name, h) {
					matched = true
					break
				}
			}
			if matched {
				capturing = true
				continue
			}
			if capturing {
				break
			}
		}
		if capturing {
			out = append(out, ln)
		}
	}
	res := strings.TrimSpace(strings.Join(out, "\n"))
	if r := []rune(res); len(r) > 1200 {
		res = string(r[:1200]) + "…"
	}
	return res
}

// weeklyHTMLSrc is the report form: a fixed Korean corporate grid (two columns —
// 실시/예정 — banded by 소관). Dynamic content is auto-escaped by html/template.
const weeklyHTMLSrc = `<!doctype html>
<html lang="ko"><head><meta charset="utf-8"><title>주간업무보고</title>
<style>
  @page { size: A4 landscape; margin: 6mm 7mm; }
  * { margin:0; padding:0; box-sizing:border-box; }
  html,body { font-family:'Noto Sans CJK KR',sans-serif; color:#000; -webkit-print-color-adjust:exact; print-color-adjust:exact; }
  body { padding:2mm 1mm; }
  .titlebar { position:relative; text-align:center; margin-bottom:6px; height:34px; }
  .title { display:inline-block; border:2px solid #000; padding:4px 34px; font-size:19px; font-weight:900; letter-spacing:10px; text-indent:10px; }
  .reporter { position:absolute; right:4px; bottom:2px; font-size:11px; }
  table.grid { width:100%; border-collapse:collapse; table-layout:fixed; }
  table.grid th, table.grid td { border:1px solid #000; vertical-align:top; }
  table.grid th { text-align:center; font-weight:700; font-size:13px; padding:5px 4px; background:#efefef; }
  .cell { font-size:9.3px; line-height:1.42; padding:5px 8px 7px; }
  .sec { font-weight:800; font-size:10.5px; margin:2px 0; }
  .sec .box { font-weight:900; margin-right:3px; }
  ul { list-style:none; margin:0; padding:0; }
  li { padding-left:11px; text-indent:-9px; margin-bottom:1.5px; }
  li::before { content:"- "; }
  .bottomhead { background:#efefef; font-weight:700; font-size:11px; padding:4px 8px; text-align:left; }
  .bottomcell { font-size:9.3px; padding:4px 8px; }
</style></head><body>
<div class="titlebar"><span class="title">{{.Office}}</span><span class="reporter">보고자 : {{.Reporter}}</span></div>
<table class="grid"><colgroup><col style="width:50%"><col style="width:50%"></colgroup>
<tr><th>실시사항 ({{.WeekDone}})</th><th>예정사항 ({{.WeekPlanned}})</th></tr>
{{range .Groups}}<tr>
<td class="cell"><div class="sec"><span class="box">▢</span>{{.Label}}</div><ul>{{range .Projects}}<li>{{.Title}}{{if .Capacity}}({{.Capacity}}){{end}} : {{.DoneLine}}</li>{{end}}</ul></td>
<td class="cell"><div class="sec"><span class="box">▢</span>{{.Label}}</div><ul>{{range .Projects}}<li>{{.Title}}{{if .Capacity}}({{.Capacity}}){{end}} : {{.PlannedLine}}</li>{{end}}</ul></td>
</tr>{{end}}
<tr><td class="bottomhead" colspan="2">현안 논의 안건</td></tr>
<tr><td class="bottomcell" colspan="2">{{if .Issues}}{{range .Issues}}- {{.}}<br>{{end}}{{else}}-{{end}}</td></tr>
<tr><td class="bottomhead" colspan="2">미진사항 및 처리계획</td></tr>
<tr><td class="bottomcell" colspan="2">-</td></tr>
</table></body></html>`
