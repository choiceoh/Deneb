// visual_render.go — shared headless-Chromium HTML→PNG render used by the chart
// and diagram tools. Both compose a self-contained dark-themed HTML page (Chart.js
// or Mermaid, embedded) and screenshot it to a PNG the agent delivers with
// send_file. This is the one place the Chromium invocation lives.
//
// Reuses the weekly-report plumbing in the same package: weeklyFindChromium (binary
// discovery), weeklyClip (error truncation). Callers handle their own output dir and
// the memory/disk headroom guards (weeklyOutputDir / weeklyCommitHeadroomMB /
// weeklyFreeDiskMB).
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// renderHTMLToPNG screenshots a local HTML file to a PNG via headless Chromium.
//
// window is the CSS-pixel canvas as "W,H"; rendered at 2x device scale so the PNG
// is twice that — crisp when the native client downscales it into a chat bubble.
// virtualTimeMs is the Chromium virtual-time budget: charts render synchronously
// (~4s is plenty) but Mermaid lays out asynchronously and needs a larger budget.
// The background is transparent so a dark card shows through cleanly.
func renderHTMLToPNG(ctx context.Context, htmlPath, pngPath, window string, virtualTimeMs int) error {
	bin := weeklyFindChromium()
	if bin == "" {
		return fmt.Errorf("chromium not found")
	}
	// Isolate Chromium's scratch on the real-disk output dir so a full tmpfs can't
	// abort the render with ENOSPC (same guard as the weekly report).
	workDir := filepath.Dir(pngPath)
	udd, err := os.MkdirTemp(workDir, "chrome-")
	if err != nil {
		return fmt.Errorf("chromium scratch dir: %w", err)
	}
	defer os.RemoveAll(udd) //nolint:errcheck // best-effort scratch cleanup
	rctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rctx, bin,
		"--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--hide-scrollbars",
		fmt.Sprintf("--virtual-time-budget=%d", virtualTimeMs),
		"--force-device-scale-factor=2",
		"--default-background-color=00000000",
		"--window-size="+window,
		"--user-data-dir="+udd, "--crash-dumps-dir="+udd,
		"--screenshot="+pngPath, "file://"+htmlPath,
	)
	cmd.Env = append(os.Environ(), "TMPDIR="+udd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chromium screenshot: %w (%s)", err, weeklyClip(string(out), 200))
	}
	if fi, err := os.Stat(pngPath); err != nil || fi.Size() == 0 {
		return fmt.Errorf("no png produced")
	}
	return nil
}
