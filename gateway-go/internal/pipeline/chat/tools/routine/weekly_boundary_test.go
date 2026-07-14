package routine

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func weeklyTestMeta(title, sogan, updated, due string, importance float64) wiki.Frontmatter {
	meta := wiki.Frontmatter{
		Title:      title,
		Summary:    title + " — 진행 상태",
		Category:   "프로젝트",
		Updated:    updated,
		Due:        due,
		Importance: importance,
	}
	if sogan != "" {
		meta.Tags = []string{"소관:" + sogan}
	}
	return meta
}

func TestCollectWeeklyComputesGroupsAndBadgesAtWeekBoundary(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 13, 14, 30, 0, 0, kstLocation) // Monday
	body := "## 진행 상황\n- 첫 작업\n- 최종 작업\n\n## 다음 액션\n- 후속 실행\n"

	writeRoutineWikiPage(t, root, "프로젝트/01-alpha/대표.md",
		weeklyTestMeta("알파 2.5MW", "1팀", "2026-07-13", "", 0.5), body)
	writeRoutineWikiPage(t, root, "프로젝트/02-beta/대표.md",
		weeklyTestMeta("베타", "2팀", "2026-07-03", "2026-07-16", 0.9), body)
	writeRoutineWikiPage(t, root, "프로젝트/03-overdue/대표.md",
		weeklyTestMeta("초과", "개인", "2026-01-01", "2026-07-06", 1.0), body)
	writeRoutineWikiPage(t, root, "프로젝트/04-today/대표.md",
		weeklyTestMeta("오늘", "2팀", "2026-07-13", "2026-07-13", 0.9), body)
	writeRoutineWikiPage(t, root, "프로젝트/05-low/대표.md",
		weeklyTestMeta("저중요도", "2팀", "2026-07-13", "2026-07-14", 0.89), body)
	writeRoutineWikiPage(t, root, "프로젝트/06-future/대표.md",
		weeklyTestMeta("미래 경계", "3팀", "2026-01-01", "2026-07-27", 0.9), body)

	stale := weeklyTestMeta("비활성", "1팀", "2026-07-02", "", 1.0)
	writeRoutineWikiPage(t, root, "프로젝트/07-stale/대표.md", stale, body)
	archived := weeklyTestMeta("보관", "1팀", "2026-07-13", "", 1.0)
	archived.Archived = true
	writeRoutineWikiPage(t, root, "프로젝트/08-archived/대표.md", archived, body)
	writeRoutineWikiPage(t, root, "프로젝트/09-untagged/대표.md",
		weeklyTestMeta("태그 없음", "", "2026-07-13", "", 1.0), body)
	writeRoutineWikiPage(t, root, "프로젝트/10-unknown/대표.md",
		weeklyTestMeta("알 수 없는 팀", "4팀", "2026-07-13", "", 1.0), body)

	env := collectWeekly(WeeklyReportOpts{WikiDir: root}, now)
	if env.WeekDone != "26.07.06~26.07.12" || env.WeekPlanned != "26.07.13~26.07.19" {
		t.Fatalf("week transition = done %q planned %q", env.WeekDone, env.WeekPlanned)
	}
	if env.GeneratedAt != "2026-07-13T14:30:00+09:00" {
		t.Fatalf("generated_at = %q", env.GeneratedAt)
	}
	wantGroups := []string{"1팀", "2팀", "3팀", "개인"}
	if len(env.Groups) != len(wantGroups) {
		t.Fatalf("groups = %+v", env.Groups)
	}
	for index, want := range wantGroups {
		if env.Groups[index].Sogan != want {
			t.Errorf("groups[%d].Sogan = %q, want %q", index, env.Groups[index].Sogan, want)
		}
	}
	if got := env.Groups[0].Projects[0]; got.Title != "알파 2.5MW" || got.Capacity != "2.5MW" || got.DoneLine != "진행 상태" || got.PlannedLine != "후속 실행" {
		t.Errorf("alpha projection = %+v", got)
	}
	if len(env.Groups[1].Projects) != 3 {
		t.Fatalf("2팀 projects = %+v", env.Groups[1].Projects)
	}
	if env.Groups[1].Projects[0].Title != "오늘" || env.Groups[1].Projects[1].Title != "저중요도" || env.Groups[1].Projects[2].Title != "베타" {
		t.Errorf("projects not sorted by updated descending, stably: %+v", env.Groups[1].Projects)
	}
	if due := env.Groups[2].Projects[0].DaysToDue; due == nil || *due != 14 {
		t.Errorf("future boundary days_to_due = %v", due)
	}

	wantIssues := []struct {
		title   string
		badge   string
		overdue bool
		days    int
	}{
		{title: "초과", badge: "기한 초과", overdue: true, days: -7},
		{title: "오늘", badge: "D-day", overdue: true, days: 0},
		{title: "베타", badge: "D-3", overdue: false, days: 3},
	}
	if len(env.IssueRows) != len(wantIssues) || len(env.Issues) != len(wantIssues) {
		t.Fatalf("issues = %v rows = %+v", env.Issues, env.IssueRows)
	}
	for index, want := range wantIssues {
		got := env.IssueRows[index]
		if got.Title != want.title || got.Badge != want.badge || got.Overdue != want.overdue || got.DaysLeft != want.days {
			t.Errorf("issue_rows[%d] = %+v, want %+v", index, got, want)
		}
	}
	for _, issue := range env.Issues {
		if strings.Contains(issue, "저중요도") {
			t.Errorf("low-importance deadline leaked into issues: %q", issue)
		}
	}
}

func TestCollectWeeklyReportDataSerializationBoundary(t *testing.T) {
	root := t.TempDir()
	writeRoutineWikiPage(t, root, "프로젝트/one/대표.md",
		weeklyTestMeta("직렬화 100kWp", "1팀", "2026-07-11", "2026-07-12", 1),
		"## 다음 액션\n- 발송\n")
	raw, err := CollectWeeklyReportData(context.Background(), WeeklyReportOpts{WikiDir: root},
		time.Date(2026, 7, 11, 12, 0, 0, 0, kstLocation))
	if err != nil {
		t.Fatalf("CollectWeeklyReportData: %v", err)
	}
	var got weeklyEnvelope
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Projects) != 1 {
		t.Fatalf("serialized groups = %+v", got.Groups)
	}
	project := got.Groups[0].Projects[0]
	if project.Capacity != "100kWp" || project.DaysToDue == nil || *project.DaysToDue != 1 {
		t.Fatalf("serialized project = %+v", project)
	}
	if !strings.Contains(raw, `"issue_rows"`) || !strings.Contains(raw, `"days_to_due": 1`) {
		t.Fatalf("missing structured fields: %s", raw)
	}
}

func TestWeeklyProjectionHelpersAtInputBoundary(t *testing.T) {
	if got := weeklySoganFromTags([]string{" misc ", " 소관: 2팀 ", "소관:1팀"}); got != "2팀" {
		t.Errorf("weeklySoganFromTags = %q", got)
	}
	if got := weeklySoganFromTags([]string{"소관:", "team:1"}); got != "" {
		t.Errorf("blank sogan = %q", got)
	}

	loc := time.FixedZone("test", 9*60*60)
	date := weeklyParseDate(" 2026-07-11T23:59:59Z ignored ", loc)
	if date.Format(time.RFC3339) != "2026-07-11T00:00:00+09:00" {
		t.Errorf("weeklyParseDate = %s", date.Format(time.RFC3339))
	}
	for _, input := range []string{"", "2026-02-30", "short"} {
		if got := weeklyParseDate(input, loc); !got.IsZero() {
			t.Errorf("weeklyParseDate(%q) = %s", input, got)
		}
	}

	capacities := map[string]string{
		"루프탑 2.5MW 착공":         "2.5MW",
		"모듈 1,200 kWp":         "1,200 kWp",
		"발전소 30㎿ 계획":           "30㎿",
		"capacity unavailable": "",
	}
	for input, want := range capacities {
		if got := weeklyCapacity(input); got != want {
			t.Errorf("weeklyCapacity(%q) = %q, want %q", input, got, want)
		}
	}
	if got := weeklyShortTitle(" \n 프로젝트 제목 \t"); got != "프로젝트 제목" {
		t.Errorf("weeklyShortTitle = %q", got)
	}
}

func TestWeeklyExtractAndBulletHelpersTruncateAtBoundary(t *testing.T) {
	body := "intro\n## 현재 진행 상황\n- first\n- second\n\n## 다음 액션 및 열린 이슈\n* [x] **ship it**\n\n## 종료\n- hidden"
	if got := weeklyExtractSection(body, "진행 상황"); got != "- first\n- second" {
		t.Errorf("progress section = %q", got)
	}
	if got := weeklyExtractSection(body, "다음 액션", "열린 이슈"); got != "* [x] **ship it**" {
		t.Errorf("next section = %q", got)
	}
	if got := weeklyExtractSection(body, "missing"); got != "" {
		t.Errorf("missing section = %q", got)
	}
	long := "## 진행 상황\n" + strings.Repeat("가", 1_205)
	if got := []rune(weeklyExtractSection(long, "진행 상황")); len(got) != 1_201 || got[len(got)-1] != '…' {
		t.Fatalf("section cap = %d runes, tail %q", len(got), string(got[len(got)-2:]))
	}

	cleanCases := map[string]string{
		"- [ ] **할 일**": "할 일",
		" * [X] 완료 ":    "완료",
		"--- 강조":        "강조",
		"plain text":    "",
		"-":             "",
	}
	for input, want := range cleanCases {
		if got := weeklyCleanBullet(input); got != want {
			t.Errorf("weeklyCleanBullet(%q) = %q, want %q", input, got, want)
		}
	}
	if got := weeklyFirstBullet("header\n- first\n- second"); got != "first" {
		t.Errorf("weeklyFirstBullet = %q", got)
	}
	if got := weeklyLastBullet("- first\nnoise\n* second"); got != "second" {
		t.Errorf("weeklyLastBullet = %q", got)
	}
	if got := weeklyDoneLine("프로젝트 — 계약 완료", "- ignored"); got != "계약 완료" {
		t.Errorf("weeklyDoneLine = %q", got)
	}
	if got := weeklyDoneLine("", "- first\n- latest"); got != "latest" {
		t.Errorf("weeklyDoneLine timeline fallback = %q", got)
	}
	if got := weeklyPlannedLine("", "summary"); got != "후속 진행" {
		t.Errorf("weeklyPlannedLine = %q", got)
	}
	if got := weeklyPlannedLine("", ""); got != "—" {
		t.Errorf("empty planned line = %q", got)
	}
	if got := weeklyClip(" \n가나다라\n ", 3); got != "가나다…" {
		t.Errorf("weeklyClip unicode = %q", got)
	}
	for _, maxLen := range []int{0, -1} {
		if got := ClipRenderError("diagnostic", maxLen); got != "" {
			t.Errorf("ClipRenderError(_, %d) = %q", maxLen, got)
		}
	}
}

func TestComposeWeeklyHTMLRendersEscapedDynamicText(t *testing.T) {
	env := weeklyEnvelope{
		Office:      `<script>alert("office")</script>`,
		Reporter:    `operator & "owner"`,
		WeekDone:    `done <unsafe>`,
		WeekPlanned: `planned & next`,
		Groups: []weeklyGroup{{
			Label: `<img src=x onerror=alert(1)>`,
			Projects: []weeklyProject{{
				Title:       `<b>project</b>`,
				DoneLine:    `done & <tag>`,
				PlannedLine: `"quoted"`,
			}},
		}},
		Issues: []string{`<script>issue</script>`},
	}
	html, err := composeWeeklyHTML(env)
	if err != nil {
		t.Fatalf("composeWeeklyHTML: %v", err)
	}
	for _, raw := range []string{"<script>alert", "<img src=x", "<b>project", "<script>issue"} {
		if strings.Contains(html, raw) {
			t.Errorf("raw dynamic HTML leaked: %q", raw)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;alert", "operator &amp;", "done &lt;unsafe&gt;", "&lt;b&gt;project"} {
		if !strings.Contains(html, escaped) {
			t.Errorf("escaped output missing %q", escaped)
		}
	}
}

func writeWeeklyPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create PNG: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("Encode PNG: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close PNG: %v", err)
	}
}

func TestWeeklyTrimPNGCropsContentAndRejectsPartialInput(t *testing.T) {
	dir := t.TempDir()
	source := image.NewRGBA(image.Rect(0, 0, 80, 60))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(20, 10, 30, 20), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	path := filepath.Join(dir, "content.png")
	writeWeeklyPNG(t, path, source)

	data, err := weeklyTrimPNG(path)
	if err != nil {
		t.Fatalf("weeklyTrimPNG: %v", err)
	}
	cropped, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("cropped output is not PNG: %v", err)
	}
	if got, want := cropped.Bounds().Size(), image.Pt(34, 32); got != want {
		t.Fatalf("cropped size = %v, want %v", got, want)
	}

	blank := image.NewRGBA(image.Rect(0, 0, 10, 10))
	draw.Draw(blank, blank.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	blankPath := filepath.Join(dir, "blank.png")
	writeWeeklyPNG(t, blankPath, blank)
	if _, err := weeklyTrimPNG(blankPath); err == nil || !strings.Contains(err.Error(), "blank image") {
		t.Fatalf("blank image err = %v", err)
	}

	partialPath := filepath.Join(dir, "partial.png")
	if err := os.WriteFile(partialPath, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := weeklyTrimPNG(partialPath); err == nil {
		t.Fatal("truncated PNG unexpectedly decoded")
	}
	if _, err := weeklyTrimPNG(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("missing PNG unexpectedly succeeded")
	}
}

func writeWeeklyExecutable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chromium-test")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile executable: %v", err)
	}
	return path
}

func TestWeeklyRenderPDFExternalProcessStatesAndCleanup(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "report.html")
	if err := os.WriteFile(htmlPath, []byte("<html>report</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("external failure is bounded", func(t *testing.T) {
		bin := writeWeeklyExecutable(t, `printf '%0300d' 0 >&2; exit 17`)
		t.Setenv("DENEB_REPORT_CHROMIUM", bin)
		err := weeklyRenderPDF(context.Background(), htmlPath, filepath.Join(dir, "failure.pdf"))
		if err == nil || !strings.Contains(err.Error(), "exit status 17") {
			t.Fatalf("external failure err = %v", err)
		}
		if len(err.Error()) > 300 {
			t.Fatalf("renderer diagnostic was not bounded: %d", len(err.Error()))
		}
	})

	t.Run("successful exit without artifact fails", func(t *testing.T) {
		bin := writeWeeklyExecutable(t, `exit 0`)
		t.Setenv("DENEB_REPORT_CHROMIUM", bin)
		err := weeklyRenderPDF(context.Background(), htmlPath, filepath.Join(dir, "missing.pdf"))
		if err == nil || !strings.Contains(err.Error(), "no pdf produced") {
			t.Fatalf("missing artifact err = %v", err)
		}
	})

	t.Run("canceled parent does not run renderer", func(t *testing.T) {
		marker := filepath.Join(dir, "marker")
		bin := writeWeeklyExecutable(t, `printf started > "`+marker+`"`)
		t.Setenv("DENEB_REPORT_CHROMIUM", bin)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := weeklyRenderPDF(ctx, htmlPath, filepath.Join(dir, "canceled.pdf"))
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("canceled render err = %v", err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("canceled renderer started, marker stat err = %v", err)
		}
	})

	if scratch, err := filepath.Glob(filepath.Join(dir, "chrome-*")); err != nil || len(scratch) != 0 {
		t.Fatalf("renderer scratch leaked: %v err=%v", scratch, err)
	}
}

func TestWeeklyFilesystemAndChromiumBoundaryHelpers(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom chromium")
	t.Setenv("DENEB_REPORT_CHROMIUM", override)
	if got := ChromiumBinary(); got != override {
		t.Fatalf("ChromiumBinary override = %q", got)
	}
	if got := weeklyFreeDiskMB(t.TempDir()); got <= 0 {
		t.Fatalf("weeklyFreeDiskMB(existing) = %d", got)
	}
	if got := weeklyFreeDiskMB(filepath.Join(t.TempDir(), "missing")); got != 1<<20 {
		t.Fatalf("weeklyFreeDiskMB(missing) = %d", got)
	}
}
