// diagram.go — render a Mermaid diagram (flowchart / gantt) as a PNG the agent can
// deliver with send_file. The sibling of the chart tool: charts turn numbers into
// graphs, diagrams turn structure/flow/schedule into a picture.
//
// Why structured args instead of raw Mermaid syntax: the model writing Mermaid by
// hand is error-prone (one bad token → broken render). Instead the tool takes a
// validated schema (nodes+edges for a flowchart, tasks for a gantt) and compiles
// the Mermaid text server-side. The agent never sees Mermaid syntax.
//
// Render path is the shared renderHTMLToPNG (headless Chromium). Unlike charts,
// diagrams have an intrinsic, variable size, so we render onto a generous dark
// canvas and trim to the content's bounding box (trimDarkPNG) for a tight card.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// mermaidJS is the Mermaid v11 UMD bundle, embedded so the render never needs
// network access. It assigns globalThis.mermaid, so a plain inline <script> exposes
// the global. ~2.5 MB.
//
//go:embed diagramassets/mermaid.min.js
var mermaidJS string

type diagramNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Shape string `json:"shape"` // rect(default) | round | stadium | diamond | circle
}

type diagramEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type ganttTask struct {
	Name     string `json:"name"`
	Section  string `json:"section"`
	Start    string `json:"start"`    // YYYY-MM-DD
	End      string `json:"end"`      // YYYY-MM-DD (alternative to duration)
	Duration string `json:"duration"` // e.g. "5d", "2w" (alternative to end)
	Status   string `json:"status"`   // done | active | crit (optional)
}

// timelineEvent is one point on a timeline: a period label and the things that
// happened then (one or more).
type timelineEvent struct {
	Section string   `json:"section"`
	Period  string   `json:"period"`
	Items   []string `json:"items"`
}

type diagramParams struct {
	DiagramType string          `json:"diagram_type"` // flowchart | gantt | timeline
	Title       string          `json:"title"`
	Subtitle    string          `json:"subtitle"`
	Direction   string          `json:"direction"` // flowchart: TD | LR (default TD)
	Nodes       []diagramNode   `json:"nodes"`
	Edges       []diagramEdge   `json:"edges"`
	Tasks       []ganttTask     `json:"tasks"`
	Events      []timelineEvent `json:"events"`
}

// ToolDiagram renders a diagram to a PNG and returns its path for send_file.
func ToolDiagram() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p diagramParams
		if err := jsonutil.UnmarshalInto("diagram params", input, &p); err != nil {
			return "", err
		}
		p.DiagramType = strings.ToLower(strings.TrimSpace(p.DiagramType))

		var mermaidDef, window string
		switch p.DiagramType {
		case "flowchart", "flow", "":
			if len(p.Nodes) == 0 {
				return "", fmt.Errorf("flowchart needs at least one node")
			}
			mermaidDef = compileFlowchart(p)
			window = "1600,2200"
		case "gantt":
			if len(p.Tasks) == 0 {
				return "", fmt.Errorf("gantt needs at least one task")
			}
			mermaidDef = compileGantt(p)
			window = "2200,1300"
		case "timeline":
			if len(p.Events) == 0 {
				return "", fmt.Errorf("timeline needs at least one event")
			}
			mermaidDef = compileTimeline(p)
			window = "2400,1600"
		default:
			return "", fmt.Errorf("unsupported diagram_type %q (use flowchart, gantt, or timeline)", p.DiagramType)
		}

		dir := weeklyOutputDir()
		if weeklyCommitHeadroomMB() < weeklyMinHeadroomMB || weeklyFreeDiskMB(dir) < weeklyMinDiskMB {
			return "", fmt.Errorf("diagram render skipped: insufficient memory/disk headroom on host")
		}

		html := buildDiagramHTML(p, mermaidDef)
		stamp := time.Now().Format("20060102-150405.000")
		htmlPath := filepath.Join(dir, "deneb-diagram-"+stamp+".html")
		pngPath := filepath.Join(dir, "deneb-diagram-"+stamp+".png")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil { //nolint:gosec // public diagram markup, not secret
			return "", fmt.Errorf("write diagram html: %w", err)
		}
		defer os.Remove(htmlPath) //nolint:errcheck // best-effort cleanup of the intermediate

		// Mermaid lays out asynchronously, so give Chromium a larger virtual-time
		// budget than a chart needs.
		if err := renderHTMLToPNG(ctx, htmlPath, pngPath, window, 10000); err != nil {
			return "", fmt.Errorf("diagram render failed: %w", err)
		}
		if err := trimDarkPNG(pngPath); err != nil {
			return "", fmt.Errorf("diagram trim failed: %w", err)
		}

		return fmt.Sprintf("다이어그램 PNG 생성됨: %s\n이제 send_file(file_path=%q, type=\"photo\", caption=\"...\")로 사용자에게 전송하세요.",
			pngPath, pngPath), nil
	}
}

// compileFlowchart turns nodes+edges into a Mermaid flowchart definition.
func compileFlowchart(p diagramParams) string {
	dir := strings.ToUpper(strings.TrimSpace(p.Direction))
	switch dir {
	case "TD", "TB", "LR", "RL", "BT":
	default:
		dir = "TD"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "flowchart %s\n", dir)
	for _, n := range p.Nodes {
		id := safeMermaidID(n.ID)
		label := mermaidLabel(n.Label)
		if label == "" {
			label = mermaidLabel(n.ID)
		}
		openB, closeB := nodeShapeDelims(n.Shape)
		fmt.Fprintf(&b, "  %s%s\"%s\"%s\n", id, openB, label, closeB)
	}
	for _, e := range p.Edges {
		from := safeMermaidID(e.From)
		to := safeMermaidID(e.To)
		if from == "" || to == "" {
			continue
		}
		if lbl := mermaidLabel(e.Label); lbl != "" {
			fmt.Fprintf(&b, "  %s -->|\"%s\"| %s\n", from, lbl, to)
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", from, to)
		}
	}
	return b.String()
}

// nodeShapeDelims returns the Mermaid bracket pair for a node shape.
func nodeShapeDelims(shape string) (openB, closeB string) {
	switch strings.ToLower(strings.TrimSpace(shape)) {
	case "round", "rounded":
		return "(", ")"
	case "stadium", "pill":
		return "([", "])"
	case "diamond", "decision":
		return "{", "}"
	case "circle":
		return "((", "))"
	default: // rect
		return "[", "]"
	}
}

// compileGantt turns tasks into a Mermaid gantt definition, grouped by section in
// first-seen order.
func compileGantt(p diagramParams) string {
	var b strings.Builder
	b.WriteString("gantt\n")
	b.WriteString("  dateFormat YYYY-MM-DD\n")
	b.WriteString("  axisFormat %m/%d\n")

	// Group tasks by section, preserving first-seen order; empty section -> "작업".
	order := []string{}
	groups := map[string][]ganttTask{}
	for _, t := range p.Tasks {
		sec := strings.TrimSpace(t.Section)
		if sec == "" {
			sec = "작업"
		}
		if _, ok := groups[sec]; !ok {
			order = append(order, sec)
		}
		groups[sec] = append(groups[sec], t)
	}

	idn := 0
	for _, sec := range order {
		fmt.Fprintf(&b, "  section %s\n", ganttText(sec))
		for _, t := range groups[sec] {
			idn++
			fmt.Fprintf(&b, "    %s :%s\n", ganttText(t.Name), ganttMeta(t, idn))
		}
	}
	return b.String()
}

// ganttMeta builds the ":" metadata for one task: [status, ]id, start, (end|dur).
func ganttMeta(t ganttTask, idn int) string {
	parts := []string{}
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case "done", "active", "crit":
		parts = append(parts, strings.ToLower(strings.TrimSpace(t.Status)))
	}
	parts = append(parts, fmt.Sprintf("t%d", idn))
	start := strings.TrimSpace(t.Start)
	if start == "" {
		start = "2026-01-01" // defensive default so the parser never chokes
	}
	parts = append(parts, start)
	switch {
	case strings.TrimSpace(t.End) != "":
		parts = append(parts, strings.TrimSpace(t.End))
	case strings.TrimSpace(t.Duration) != "":
		parts = append(parts, strings.TrimSpace(t.Duration))
	default:
		parts = append(parts, "1d")
	}
	return strings.Join(parts, ", ")
}

// compileTimeline turns events into a Mermaid timeline definition, grouped by
// section in first-seen order. Each event is "period : item : item …"; colons are
// the separator, so they are stripped from the text.
func compileTimeline(p diagramParams) string {
	var b strings.Builder
	b.WriteString("timeline\n")

	order := []string{}
	groups := map[string][]timelineEvent{}
	sectioned := false
	for _, e := range p.Events {
		sec := strings.TrimSpace(e.Section)
		if sec != "" {
			sectioned = true
		}
		if _, ok := groups[sec]; !ok {
			order = append(order, sec)
		}
		groups[sec] = append(groups[sec], e)
	}

	emit := func(e timelineEvent) {
		period := ganttText(e.Period)
		if period == "" {
			period = " "
		}
		parts := []string{period}
		for _, it := range e.Items {
			if t := ganttText(it); t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) == 1 { // period with no items — show the period as its own event
			parts = append(parts, period)
		}
		fmt.Fprintf(&b, "    %s\n", strings.Join(parts, " : "))
	}

	for _, sec := range order {
		if sectioned {
			name := sec
			if name == "" {
				name = "·"
			}
			fmt.Fprintf(&b, "  section %s\n", ganttText(name))
		}
		for _, e := range groups[sec] {
			emit(e)
		}
	}
	return b.String()
}

// safeMermaidID maps an arbitrary id string to a valid Mermaid node id
// (alphanumeric + underscore). Deterministic, so node defs and edge refs match.
func safeMermaidID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r >= 0x80: // keep CJK and other letters as-is; Mermaid accepts unicode ids
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if c := out[0]; c >= '0' && c <= '9' {
		out = "n" + out
	}
	return out
}

// mermaidLabel sanitizes a node/edge label for use inside a quoted Mermaid string.
// Double quotes would close the string early, so swap them for typographic quotes.
func mermaidLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, `"`, "”")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// ganttText sanitizes a gantt section/task name. Commas and colons are gantt
// metadata separators, so strip them from the display text.
func ganttText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(",", " ", ":", " ", "\n", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// buildDiagramHTML wraps the Mermaid definition in a dark-themed page with the
// Mermaid runtime inlined.
func buildDiagramHTML(p diagramParams, def string) string {
	subtitle := ""
	if s := strings.TrimSpace(p.Subtitle); s != "" {
		subtitle = `<div class="subtitle">` + htmlEscape(s) + `</div>`
	}
	titleBlock := ""
	if t := strings.TrimSpace(p.Title); t != "" || subtitle != "" {
		titleBlock = `<div class="head"><div class="title">` + htmlEscape(t) + `</div>` + subtitle + `</div>`
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html lang="ko"><head><meta charset="utf-8">
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html,body { background:%s; font-family:'Noto Sans CJK KR','Noto Sans KR',sans-serif;
    -webkit-font-smoothing:antialiased; }
  .card { display:inline-block; padding:30px 34px 32px; }
  .head { margin-bottom:18px; }
  .title { color:%s; font-size:25px; font-weight:700; letter-spacing:-0.4px; line-height:1.25; }
  .subtitle { color:%s; font-size:14px; font-weight:400; margin-top:5px; letter-spacing:-0.2px; }
  .mermaid { color:%s; }
</style></head><body>
<div class="card">%s<div class="mermaid">%s</div></div>
<script>%s</script>
<script>
  mermaid.initialize({
    startOnLoad: true,
    theme: 'dark',
    securityLevel: 'loose',
    fontFamily: "'Noto Sans CJK KR','Noto Sans KR',sans-serif",
    themeVariables: { background: '%s', fontFamily: "'Noto Sans CJK KR','Noto Sans KR',sans-serif" },
    flowchart: { curve: 'basis', padding: 14, useMaxWidth: false },
    gantt: { useMaxWidth: false, useWidth: 1500, barHeight: 26, barGap: 8,
      topPadding: 40, gridLineStartPadding: 36, leftPadding: 130, fontSize: 13,
      tickInterval: '1week' },
    timeline: { useMaxWidth: false, padding: 14 }
  });
</script>
</body></html>`,
		chartBg, chartFg, chartMuted, chartFg,
		titleBlock, htmlEscape(def), mermaidJS, chartBg)
	return b.String()
}

// trimDarkPNG crops the rendered PNG to the bounding box of non-background pixels
// (the dark card extends to the full render canvas) and rewrites it in place.
func trimDarkPNG(pngPath string) error {
	f, err := os.Open(pngPath)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	_ = f.Close() //nolint:errcheck // read-only
	if err != nil {
		return err
	}
	b := src.Bounds()
	// Background is the card colour #0A0A0B (10,10,11). A pixel belongs to content
	// when any 8-bit channel differs from the background by more than this — large
	// enough to ignore antialiasing fuzz, small enough to keep faint hairlines.
	const bgR, bgG, bgB = 10, 10, 11
	const thresh = 26
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			if absDiff(int(r>>8), bgR) > thresh || absDiff(int(g>>8), bgG) > thresh || absDiff(int(bl>>8), bgB) > thresh {
				found = true
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("blank diagram")
	}
	const margin = 40 // 2x scale, so ~20 CSS px of breathing room
	minX = maxInt(b.Min.X, minX-margin)
	minY = maxInt(b.Min.Y, minY-margin)
	maxX = minInt(b.Max.X, maxX+margin+1)
	maxY = minInt(b.Max.Y, maxY+margin+1)
	crop := image.NewRGBA(image.Rect(0, 0, maxX-minX, maxY-minY))
	draw.Draw(crop, crop.Bounds(), src, image.Point{X: minX, Y: minY}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, crop); err != nil {
		return err
	}
	return os.WriteFile(pngPath, buf.Bytes(), 0o644) //nolint:gosec // public diagram image
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
