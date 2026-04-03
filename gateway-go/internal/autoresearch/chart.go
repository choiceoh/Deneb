package autoresearch

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

// Chart dimensions and layout constants.
// Using 4x resolution for legibility on Telegram (mobile compression destroys small text).
const (
	chartWidth  = 3200
	chartHeight = 2000

	// Margins around the plot area (scaled for 4x font).
	marginTop    = 180
	marginBottom = 240
	marginLeft   = 320
	marginRight  = 120

	// Derived plot area.
	plotLeft   = marginLeft
	plotTop    = marginTop
	plotRight  = chartWidth - marginRight
	plotBottom = chartHeight - marginBottom

	pointRadius = 16

	// Font scale for all text rendering.
	fontScale = 4
)

// Color palette.
var (
	colBackground = color.RGBA{255, 255, 255, 255}
	colGrid       = color.RGBA{224, 224, 224, 255}
	colAxis       = color.RGBA{51, 51, 51, 255}
	colKept       = color.RGBA{46, 204, 64, 255}  // green
	colDiscarded  = color.RGBA{255, 65, 54, 255}   // red
	colBestLine   = color.RGBA{0, 116, 217, 255}   // blue
	colText       = color.RGBA{17, 17, 17, 255}
	colLegendBg   = color.RGBA{245, 245, 245, 255}
)

// RenderChart generates a PNG chart from autoresearch results.
func RenderChart(rows []ResultRow, cfg *Config) ([]byte, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no results to chart")
	}

	img := image.NewRGBA(image.Rect(0, 0, chartWidth, chartHeight))

	// Fill background.
	fillRect(img, 0, 0, chartWidth, chartHeight, colBackground)

	// Compute data ranges.
	minIter, maxIter := rows[0].Iteration, rows[0].Iteration
	minVal, maxVal := rows[0].MetricValue, rows[0].MetricValue
	for _, r := range rows {
		if r.Iteration < minIter {
			minIter = r.Iteration
		}
		if r.Iteration > maxIter {
			maxIter = r.Iteration
		}
		if r.MetricValue < minVal {
			minVal = r.MetricValue
		}
		if r.MetricValue > maxVal {
			maxVal = r.MetricValue
		}
		// Include best_so_far in range.
		if r.BestSoFar < minVal {
			minVal = r.BestSoFar
		}
		if r.BestSoFar > maxVal {
			maxVal = r.BestSoFar
		}
	}

	// Add 5% padding to y-range.
	yRange := maxVal - minVal
	if yRange == 0 {
		yRange = 1.0
	}
	yPad := yRange * 0.08
	minVal -= yPad
	maxVal += yPad

	// Ensure x-range is at least 1.
	if maxIter == minIter {
		maxIter = minIter + 1
	}

	// Mapping functions.
	mapX := func(iter int) int {
		return plotLeft + int(float64(iter-minIter)/float64(maxIter-minIter)*float64(plotRight-plotLeft))
	}
	mapY := func(val float64) int {
		// Invert y: higher values at top.
		return plotBottom - int((val-minVal)/(maxVal-minVal)*float64(plotBottom-plotTop))
	}

	// Draw grid and axes.
	drawGrid(img, minVal, maxVal, minIter, maxIter, mapX, mapY)
	drawAxes(img)

	// Draw best-so-far step line.
	drawBestLine(img, rows, mapX, mapY)

	// Draw data points (on top of line).
	for _, r := range rows {
		x := mapX(r.Iteration)
		y := mapY(r.MetricValue)
		c := colDiscarded
		if r.Kept {
			c = colKept
		}
		fillCircle(img, x, y, pointRadius, c)
		// Dark outline for visibility.
		drawCircleOutline(img, x, y, pointRadius, colAxis)
	}

	// Draw title.
	title := cfg.MetricName
	if cfg.MetricDirection != "" {
		title += " (" + cfg.MetricDirection + ")"
	}
	charW := 6 * fontScale // glyph width * scale
	charH := 7 * fontScale
	titleX := (chartWidth - len(title)*charW) / 2
	drawString(img, titleX, 40, title, colText, fontScale)

	// Draw axis labels.
	axisLabel := "iteration"
	drawString(img, (plotLeft+plotRight)/2-len(axisLabel)*charW/2, chartHeight-48, axisLabel, colAxis, fontScale)

	// Draw x-axis tick labels.
	xTicks := niceTicksInt(minIter, maxIter, 10)
	for _, tick := range xTicks {
		x := mapX(tick)
		label := strconv.Itoa(tick)
		drawString(img, x-len(label)*charW/2, plotBottom+40, label, colAxis, fontScale)
	}

	// Draw y-axis tick labels.
	yTicks := niceTicksFloat(minVal, maxVal, 6)
	for _, tick := range yTicks {
		y := mapY(tick)
		label := formatTickLabel(tick)
		drawString(img, plotLeft-len(label)*charW-20, y-charH/2, label, colAxis, fontScale)
	}

	// Draw legend.
	drawLegend(img)

	// Encode PNG.
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// ChartPath returns the path to the chart PNG inside the workspace.
func ChartPath(workdir string) string {
	return filepath.Join(workdir, configDir, "chart.png")
}

// SaveChart generates and saves the chart, returning the file path.
func SaveChart(workdir string, rows []ResultRow, cfg *Config) (string, error) {
	data, err := RenderChart(rows, cfg)
	if err != nil {
		return "", err
	}
	path := ChartPath(workdir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create chart dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write chart: %w", err)
	}
	return path, nil
}

// --- Drawing primitives ---

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func drawHLine(img *image.RGBA, x0, x1, y int, c color.RGBA) {
	if y < 0 || y >= chartHeight {
		return
	}
	for x := x0; x <= x1; x++ {
		if x >= 0 && x < chartWidth {
			img.SetRGBA(x, y, c)
		}
	}
}

func drawVLine(img *image.RGBA, x, y0, y1 int, c color.RGBA) {
	if x < 0 || x >= chartWidth {
		return
	}
	for y := y0; y <= y1; y++ {
		if y >= 0 && y < chartHeight {
			img.SetRGBA(x, y, c)
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	// Bresenham's line algorithm.
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= 0 && x0 < chartWidth && y0 >= 0 && y0 < chartHeight {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// drawThickLine draws a line with thickness by drawing parallel lines.
func drawThickLine(img *image.RGBA, x0, y0, x1, y1, thickness int, c color.RGBA) {
	for d := -thickness / 2; d <= thickness/2; d++ {
		if abs(y1-y0) > abs(x1-x0) {
			drawLine(img, x0+d, y0, x1+d, y1, c)
		} else {
			drawLine(img, x0, y0+d, x1, y1+d, c)
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px, py := cx+x, cy+y
				if px >= 0 && px < chartWidth && py >= 0 && py < chartHeight {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
}

func drawCircleOutline(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := -r - 1; y <= r+1; y++ {
		for x := -r - 1; x <= r+1; x++ {
			dist := x*x + y*y
			if dist >= r*r-r && dist <= r*r+r+1 {
				px, py := cx+x, cy+y
				if px >= 0 && px < chartWidth && py >= 0 && py < chartHeight {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
}

// --- Chart components ---

func drawAxes(img *image.RGBA) {
	// X axis.
	drawHLine(img, plotLeft, plotRight, plotBottom, colAxis)
	// Y axis.
	drawVLine(img, plotLeft, plotTop, plotBottom, colAxis)
}

func drawGrid(img *image.RGBA, minVal, maxVal float64, minIter, maxIter int,
	mapX func(int) int, mapY func(float64) int) {
	// Horizontal grid lines at y-ticks.
	yTicks := niceTicksFloat(minVal, maxVal, 6)
	for _, tick := range yTicks {
		y := mapY(tick)
		if y > plotTop && y < plotBottom {
			drawHLine(img, plotLeft+1, plotRight, y, colGrid)
		}
	}
	// Vertical grid lines at x-ticks.
	xTicks := niceTicksInt(minIter, maxIter, 10)
	for _, tick := range xTicks {
		x := mapX(tick)
		if x > plotLeft && x < plotRight {
			drawVLine(img, x, plotTop, plotBottom-1, colGrid)
		}
	}
}

func drawBestLine(img *image.RGBA, rows []ResultRow,
	mapX func(int) int, mapY func(float64) int) {
	if len(rows) < 2 {
		return
	}
	for i := 1; i < len(rows); i++ {
		x0 := mapX(rows[i-1].Iteration)
		y0 := mapY(rows[i-1].BestSoFar)
		x1 := mapX(rows[i].Iteration)
		y1 := mapY(rows[i].BestSoFar)
		// Step line: horizontal then vertical.
		drawThickLine(img, x0, y0, x1, y0, 6, colBestLine)
		drawThickLine(img, x1, y0, x1, y1, 6, colBestLine)
	}
}

func drawLegend(img *image.RGBA) {
	charW := 6 * fontScale
	charH := 7 * fontScale
	// Legend box at bottom-right.
	lx := plotRight - 880
	ly := chartHeight - charH - 32
	fillRect(img, lx, ly-8, lx+860, ly+charH+8, colLegendBg)

	// Kept indicator.
	fillCircle(img, lx+24, ly+charH/2, 12, colKept)
	drawString(img, lx+48, ly, "kept", colText, fontScale)

	// Discarded indicator.
	dx := lx + 48 + 4*charW + 40
	fillCircle(img, dx+24, ly+charH/2, 12, colDiscarded)
	drawString(img, dx+48, ly, "discarded", colText, fontScale)

	// Best line indicator.
	bx := dx + 48 + 9*charW + 40
	drawThickLine(img, bx, ly+charH/2, bx+40, ly+charH/2, 6, colBestLine)
	drawString(img, bx+56, ly, "best", colText, fontScale)
}

// --- Tick computation ---

func niceTicksFloat(min, max float64, maxTicks int) []float64 {
	rang := max - min
	if rang <= 0 {
		return []float64{min}
	}
	rawStep := rang / float64(maxTicks)
	// Round step to a nice value.
	mag := math.Pow(10, math.Floor(math.Log10(rawStep)))
	normalized := rawStep / mag
	var niceStep float64
	switch {
	case normalized <= 1.5:
		niceStep = 1 * mag
	case normalized <= 3.5:
		niceStep = 2 * mag
	case normalized <= 7.5:
		niceStep = 5 * mag
	default:
		niceStep = 10 * mag
	}

	start := math.Ceil(min/niceStep) * niceStep
	var ticks []float64
	// Use index-based iteration to avoid floating-point accumulation errors.
	for i := 0; i <= 1000; i++ {
		v := start + float64(i)*niceStep
		if v > max+niceStep*1e-9 {
			break
		}
		ticks = append(ticks, v)
	}
	return ticks
}

func niceTicksInt(min, max, maxTicks int) []int {
	rang := max - min
	if rang <= 0 {
		return []int{min}
	}
	rawStep := float64(rang) / float64(maxTicks)
	step := int(math.Max(1, math.Round(rawStep)))
	// Round step to a nice value.
	if step > 2 {
		nice := []int{1, 2, 5, 10, 20, 25, 50, 100, 200, 500, 1000}
		for _, n := range nice {
			if n >= step {
				step = n
				break
			}
		}
	}
	start := ((min + step - 1) / step) * step
	if start > min+step/2 {
		start = min
	}
	var ticks []int
	for v := start; v <= max; v += step {
		ticks = append(ticks, v)
	}
	return ticks
}

func formatTickLabel(v float64) string {
	// Use minimal precision needed.
	av := math.Abs(v)
	switch {
	case av == 0:
		return "0"
	case av >= 100:
		return strconv.Itoa(int(math.Round(v)))
	case av >= 1:
		return strconv.FormatFloat(v, 'f', 2, 64)
	case av >= 0.01:
		return strconv.FormatFloat(v, 'f', 3, 64)
	default:
		return strconv.FormatFloat(v, 'f', 4, 64)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// --- Bitmap font (5x7) ---

// drawString renders ASCII text using a simple 5x7 bitmap font.
// scale multiplies the pixel size (1 = normal, 2 = double).
func drawString(img *image.RGBA, x, y int, s string, c color.RGBA, scale int) {
	cx := x
	for _, ch := range s {
		glyph := bitmapGlyph(byte(ch))
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if glyph[row]&(1<<(4-col)) != 0 {
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							px := cx + col*scale + dx
							py := y + row*scale + dy
							if px >= 0 && px < chartWidth && py >= 0 && py < chartHeight {
								img.SetRGBA(px, py, c)
							}
						}
					}
				}
			}
		}
		cx += 6 * scale // 5px glyph + 1px spacing
	}
}

// bitmapGlyph returns a 7-row bitmap for an ASCII character.
// Each row is a byte where the top 5 bits represent pixels.
func bitmapGlyph(ch byte) [7]byte {
	if g, ok := glyphs[ch]; ok {
		return g
	}
	return glyphs['?']
}

// glyphs maps ASCII characters to 5x7 bitmaps.
// Each byte: bits 7-3 = pixel columns left to right (bit 4 = leftmost).
var glyphs = map[byte][7]byte{
	' ': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	'0': {0x70, 0x88, 0x98, 0xA8, 0xC8, 0x88, 0x70},
	'1': {0x20, 0x60, 0x20, 0x20, 0x20, 0x20, 0x70},
	'2': {0x70, 0x88, 0x08, 0x10, 0x20, 0x40, 0xF8},
	'3': {0xF8, 0x10, 0x20, 0x10, 0x08, 0x88, 0x70},
	'4': {0x10, 0x30, 0x50, 0x90, 0xF8, 0x10, 0x10},
	'5': {0xF8, 0x80, 0xF0, 0x08, 0x08, 0x88, 0x70},
	'6': {0x30, 0x40, 0x80, 0xF0, 0x88, 0x88, 0x70},
	'7': {0xF8, 0x08, 0x10, 0x20, 0x40, 0x40, 0x40},
	'8': {0x70, 0x88, 0x88, 0x70, 0x88, 0x88, 0x70},
	'9': {0x70, 0x88, 0x88, 0x78, 0x08, 0x10, 0x60},
	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x60, 0x60},
	'-': {0x00, 0x00, 0x00, 0xF8, 0x00, 0x00, 0x00},
	'(': {0x10, 0x20, 0x40, 0x40, 0x40, 0x20, 0x10},
	')': {0x40, 0x20, 0x10, 0x10, 0x10, 0x20, 0x40},
	'/': {0x00, 0x08, 0x10, 0x20, 0x40, 0x80, 0x00},
	'_': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8},
	':': {0x00, 0x60, 0x60, 0x00, 0x60, 0x60, 0x00},
	'%': {0xC8, 0xC8, 0x10, 0x20, 0x40, 0x98, 0x98},
	'+': {0x00, 0x20, 0x20, 0xF8, 0x20, 0x20, 0x00},
	'?': {0x70, 0x88, 0x08, 0x10, 0x20, 0x00, 0x20},

	'A': {0x70, 0x88, 0x88, 0xF8, 0x88, 0x88, 0x88},
	'B': {0xF0, 0x88, 0x88, 0xF0, 0x88, 0x88, 0xF0},
	'C': {0x70, 0x88, 0x80, 0x80, 0x80, 0x88, 0x70},
	'D': {0xE0, 0x90, 0x88, 0x88, 0x88, 0x90, 0xE0},
	'E': {0xF8, 0x80, 0x80, 0xF0, 0x80, 0x80, 0xF8},
	'F': {0xF8, 0x80, 0x80, 0xF0, 0x80, 0x80, 0x80},
	'G': {0x70, 0x88, 0x80, 0xB8, 0x88, 0x88, 0x70},
	'H': {0x88, 0x88, 0x88, 0xF8, 0x88, 0x88, 0x88},
	'I': {0x70, 0x20, 0x20, 0x20, 0x20, 0x20, 0x70},
	'J': {0x38, 0x10, 0x10, 0x10, 0x10, 0x90, 0x60},
	'K': {0x88, 0x90, 0xA0, 0xC0, 0xA0, 0x90, 0x88},
	'L': {0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0xF8},
	'M': {0x88, 0xD8, 0xA8, 0xA8, 0x88, 0x88, 0x88},
	'N': {0x88, 0xC8, 0xA8, 0x98, 0x88, 0x88, 0x88},
	'O': {0x70, 0x88, 0x88, 0x88, 0x88, 0x88, 0x70},
	'P': {0xF0, 0x88, 0x88, 0xF0, 0x80, 0x80, 0x80},
	'Q': {0x70, 0x88, 0x88, 0x88, 0xA8, 0x90, 0x68},
	'R': {0xF0, 0x88, 0x88, 0xF0, 0xA0, 0x90, 0x88},
	'S': {0x70, 0x88, 0x80, 0x70, 0x08, 0x88, 0x70},
	'T': {0xF8, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20},
	'U': {0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x70},
	'V': {0x88, 0x88, 0x88, 0x88, 0x88, 0x50, 0x20},
	'W': {0x88, 0x88, 0x88, 0xA8, 0xA8, 0xD8, 0x88},
	'X': {0x88, 0x88, 0x50, 0x20, 0x50, 0x88, 0x88},
	'Y': {0x88, 0x88, 0x50, 0x20, 0x20, 0x20, 0x20},
	'Z': {0xF8, 0x08, 0x10, 0x20, 0x40, 0x80, 0xF8},

	'a': {0x00, 0x00, 0x70, 0x08, 0x78, 0x88, 0x78},
	'b': {0x80, 0x80, 0xB0, 0xC8, 0x88, 0xC8, 0xB0},
	'c': {0x00, 0x00, 0x70, 0x80, 0x80, 0x88, 0x70},
	'd': {0x08, 0x08, 0x68, 0x98, 0x88, 0x98, 0x68},
	'e': {0x00, 0x00, 0x70, 0x88, 0xF8, 0x80, 0x70},
	'f': {0x30, 0x48, 0x40, 0xE0, 0x40, 0x40, 0x40},
	'g': {0x00, 0x00, 0x68, 0x98, 0x78, 0x08, 0x70},
	'h': {0x80, 0x80, 0xB0, 0xC8, 0x88, 0x88, 0x88},
	'i': {0x20, 0x00, 0x60, 0x20, 0x20, 0x20, 0x70},
	'j': {0x10, 0x00, 0x30, 0x10, 0x10, 0x90, 0x60},
	'k': {0x80, 0x80, 0x90, 0xA0, 0xC0, 0xA0, 0x90},
	'l': {0x60, 0x20, 0x20, 0x20, 0x20, 0x20, 0x70},
	'm': {0x00, 0x00, 0xD0, 0xA8, 0xA8, 0x88, 0x88},
	'n': {0x00, 0x00, 0xB0, 0xC8, 0x88, 0x88, 0x88},
	'o': {0x00, 0x00, 0x70, 0x88, 0x88, 0x88, 0x70},
	'p': {0x00, 0x00, 0xB0, 0xC8, 0xF0, 0x80, 0x80},
	'q': {0x00, 0x00, 0x68, 0x98, 0x78, 0x08, 0x08},
	'r': {0x00, 0x00, 0xB0, 0xC8, 0x80, 0x80, 0x80},
	's': {0x00, 0x00, 0x78, 0x80, 0x70, 0x08, 0xF0},
	't': {0x40, 0x40, 0xE0, 0x40, 0x40, 0x48, 0x30},
	'u': {0x00, 0x00, 0x88, 0x88, 0x88, 0x98, 0x68},
	'v': {0x00, 0x00, 0x88, 0x88, 0x88, 0x50, 0x20},
	'w': {0x00, 0x00, 0x88, 0x88, 0xA8, 0xA8, 0x50},
	'x': {0x00, 0x00, 0x88, 0x50, 0x20, 0x50, 0x88},
	'y': {0x00, 0x00, 0x88, 0x88, 0x78, 0x08, 0x70},
	'z': {0x00, 0x00, 0xF8, 0x10, 0x20, 0x40, 0xF8},
}
