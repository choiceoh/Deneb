// visual_render.go — shared headless-Chromium HTML→PNG render used by the chart
// and diagram tools. Both compose a self-contained dark-themed HTML page (Chart.js
// or Mermaid, embedded) and screenshot it to a PNG the agent delivers with
// send_file. This is the one place the Chromium invocation lives.
//
// Chromium discovery and error clipping stay local. Callers use the routine
// package's shared output directory and memory/disk headroom guard.
package artifact

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
)

// finishRenderedImage is the common tail of the chart/diagram tools: with
// send=false it returns the classic "path + call send_file" instruction; with
// send=true it delivers the PNG in the same call (photo, caption defaulting to
// the chart/diagram title), removing the second round-trip the model sometimes
// forgets. Delivery failure degrades to the send_file instruction — the render
// itself succeeded, so never fail the call at this point.
func finishRenderedImage(ctx context.Context, pngPath, kind string, send bool, caption, title string) string {
	if !send {
		return fmt.Sprintf("%s PNG 생성됨: %s\n이제 send_file(file_path=%q, type=\"photo\", caption=\"...\")로 사용자에게 전송하세요.",
			kind, pngPath, pngPath)
	}
	capText := caption
	if capText == "" {
		capText = title
	}
	if err := deliverRenderedImage(ctx, pngPath, capText); err != nil {
		return fmt.Sprintf("%s PNG 생성됨: %s\n자동 전송 실패(%s) — send_file(file_path=%q, type=\"photo\")로 직접 전송하세요.",
			kind, pngPath, err, pngPath)
	}
	result := fmt.Sprintf("%s PNG 생성 + 전송 완료: %s", kind, pngPath)
	if info, err := os.Stat(pngPath); err == nil {
		if vpath := archiveSentFile(ctx, pngPath, info.Size()); vpath != "" {
			result += "; 파일 저장소 보관: " + vpath
		}
	}
	return result
}

// deliverRenderedImage sends a just-rendered PNG over the active channel.
func deliverRenderedImage(ctx context.Context, pngPath, caption string) error {
	sendFn := toolport.MediaSendFuncFromContext(ctx)
	delivery := toolport.DeliveryFromContext(ctx)
	if sendFn == nil || delivery == nil || delivery.Channel == "" || delivery.To == "" {
		return fmt.Errorf("채널 미연결/배달 대상 없음")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return sendFn(sendCtx, delivery, pngPath, "photo", caption, false)
}

// renderHTMLToPNG screenshots a local HTML file to a PNG via headless Chromium.
//
// window is the CSS-pixel canvas as "W,H"; rendered at 2x device scale so the PNG
// is twice that — crisp when the native client downscales it into a chat bubble.
// virtualTimeMs is the Chromium virtual-time budget: charts render synchronously
// (~4s is plenty) but Mermaid lays out asynchronously and needs a larger budget.
// The background is transparent so a dark card shows through cleanly.
func renderHTMLToPNG(ctx context.Context, htmlPath, pngPath, window string, virtualTimeMs int) error {
	bin := routine.ChromiumBinary()
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
	cmd := exec.CommandContext(
		rctx, bin,
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
		return fmt.Errorf("chromium screenshot: %w (%s)", err, routine.ClipRenderError(string(out), 200))
	}
	if fi, err := os.Stat(pngPath); err != nil || fi.Size() == 0 {
		return fmt.Errorf("no png produced")
	}
	return nil
}
