// watch.go — the "watch a video" tool: let the agent SEE and HEAR a video.
//
// The agent's normal YouTube path (web tool) reads only the subtitle transcript,
// so the model never sees the screen. This tool closes that gap: given a YouTube
// URL or a local video file, it extracts representative frames + subtitles
// (media.WatchVideo) and analyzes them with the main multimodal model in an
// ISOLATED vision call (pilot.CallVisionLLM). Only the resulting analysis text
// flows back into the conversation — the base64 frames never touch the main
// transcript, preserving the prompt cache and context budget (the same isolation
// rationale as the YouTube transcript summarizer in web_youtube.go).
//
// Typical uses: analyze a video's structure/hook, diagnose a bug from a screen
// recording, or summarize a long video faster than watching at 2x.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// WatchParams is the input schema for the watch tool.
type WatchParams struct {
	Source string  `json:"source"`          // YouTube URL or local video file path
	Task   string  `json:"task,omitempty"`  // what to analyze (default: general analysis)
	Start  float64 `json:"start,omitempty"` // optional window start (seconds)
	End    float64 `json:"end,omitempty"`   // optional window end (seconds)
}

const (
	// watchMaxTranscriptChars caps the subtitle text fed to the vision model so
	// a long transcript cannot crowd out the frames in the analysis call.
	watchMaxTranscriptChars = 12000
	// watchAnalysisMaxTokens is the token budget for the generated analysis.
	watchAnalysisMaxTokens = 1500
)

const watchSystemPrompt = "당신은 영상을 분석하는 전문가입니다. " +
	"제공된 프레임(시간순 캡처)과 자막을 바탕으로 영상을 실제로 본 것처럼 분석하세요. " +
	"화면에 보이는 것, 진행 흐름, 핵심 장면을 구체적으로 설명하고, 자막이 있으면 내용과 연결하세요. " +
	"요청된 작업이 있으면 그에 집중하세요. 불필요한 서두 없이 한국어로 분석 결과만 출력하세요."

// ToolWatch returns a ToolFunc that watches (frames + subtitles + vision
// analysis) a YouTube URL or a local video file. workspaceDir bounds local file
// access; an empty string disables local-file watching (URL-only).
func ToolWatch(workspaceDir string) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p WatchParams
		if err := jsonutil.UnmarshalInto("watch params", input, &p); err != nil {
			return "", err
		}
		p.Source = strings.TrimSpace(p.Source)
		if p.Source == "" {
			return "", fmt.Errorf("source는 필수입니다 (유튜브 URL 또는 영상 파일 경로)")
		}

		// Local files are jailed to the workspace and screened by the
		// prompt-injection path guard, mirroring the fs tools.
		if !media.IsYouTubeURL(p.Source) {
			if workspaceDir == "" {
				return "", fmt.Errorf("로컬 영상 파일 분석이 비활성화되어 있습니다. 유튜브 URL을 사용하세요")
			}
			resolved := ResolvePath(p.Source, workspaceDir)
			if err := CheckProtectedPath(resolved, "read"); err != nil {
				return "", err
			}
			p.Source = resolved
		}

		result, err := media.WatchVideo(ctx, p.Source, media.WatchOptions{
			StartSec: p.Start,
			EndSec:   p.End,
		})
		if err != nil {
			return "", fmt.Errorf("영상 처리 실패: %w", err)
		}
		if len(result.Frames) == 0 {
			return "", fmt.Errorf("영상에서 프레임을 추출하지 못했습니다 (ffmpeg 설치 여부 확인)")
		}

		analysis, err := analyzeWatch(ctx, &p, result)
		if err != nil {
			// Frames extracted but vision call failed — return a useful
			// metadata + transcript fallback rather than a bare error so the
			// user still gets something actionable.
			return formatWatchFallback(result, err), nil
		}
		return formatWatchResult(result, analysis), nil
	}
}

// analyzeWatch runs the isolated vision call over the extracted frames.
func analyzeWatch(ctx context.Context, p *WatchParams, result *media.WatchResult) (string, error) {
	frames := make([]pilot.VisionFrame, 0, len(result.Frames))
	for _, f := range result.Frames {
		frames = append(frames, pilot.VisionFrame{
			MimeType: "image/jpeg",
			Base64:   base64.StdEncoding.EncodeToString(f),
		})
	}

	userText := buildWatchPrompt(p, result)
	return pilot.CallVisionLLM(ctx, watchSystemPrompt, userText, frames, watchAnalysisMaxTokens)
}

// buildWatchPrompt assembles the per-call prompt: task + metadata + (clipped)
// transcript. The frames follow as image blocks (added by CallVisionLLM).
func buildWatchPrompt(p *WatchParams, result *media.WatchResult) string {
	var b strings.Builder
	task := strings.TrimSpace(p.Task)
	if task == "" {
		task = "이 영상의 전체 내용과 구조를 분석해줘."
	}
	fmt.Fprintf(&b, "작업: %s\n\n", task)

	if result.Title != "" {
		fmt.Fprintf(&b, "영상: %s\n", result.Title)
	}
	if result.Channel != "" {
		fmt.Fprintf(&b, "채널: %s\n", result.Channel)
	}
	if result.DurationSec > 0 {
		fmt.Fprintf(&b, "길이: %s\n", formatWatchDuration(result.DurationSec))
	}
	if result.EndSec > 0 || result.StartSec > 0 {
		fmt.Fprintf(&b, "분석 구간: %.0fs ~ %.0fs\n", result.StartSec, result.EndSec)
	}
	fmt.Fprintf(&b, "프레임: %d장 (시간순)\n", len(result.Frames))

	if t := strings.TrimSpace(result.Transcript); t != "" {
		if len(t) > watchMaxTranscriptChars {
			t = t[:watchMaxTranscriptChars] + "\n[자막 일부 생략]"
		}
		fmt.Fprintf(&b, "\n자막(%s):\n%s\n", result.Language, t)
	} else {
		b.WriteString("\n(자막 없음 — 프레임만으로 분석)\n")
	}
	return b.String()
}

// formatWatchResult renders the final tool output: a header + the analysis.
func formatWatchResult(result *media.WatchResult, analysis string) string {
	var b strings.Builder
	b.WriteString("## 🎬 영상 분석\n\n")
	if result.Title != "" {
		fmt.Fprintf(&b, "**%s**", result.Title)
		if result.Channel != "" {
			fmt.Fprintf(&b, " — %s", result.Channel)
		}
		b.WriteString("\n")
	}
	meta := []string{fmt.Sprintf("프레임 %d장", len(result.Frames))}
	if result.DurationSec > 0 {
		meta = append(meta, formatWatchDuration(result.DurationSec))
	}
	if strings.TrimSpace(result.Transcript) != "" {
		meta = append(meta, "자막 있음")
	}
	fmt.Fprintf(&b, "_%s_\n\n", strings.Join(meta, " · "))
	b.WriteString(strings.TrimSpace(analysis))
	b.WriteString("\n")
	return b.String()
}

// formatWatchFallback renders a degraded result when the vision call fails but
// frames/transcript were extracted.
func formatWatchFallback(result *media.WatchResult, callErr error) string {
	var b strings.Builder
	b.WriteString("## 🎬 영상 (분석 모델 사용 불가)\n\n")
	if result.Title != "" {
		fmt.Fprintf(&b, "**%s**\n", result.Title)
	}
	fmt.Fprintf(&b, "프레임 %d장을 추출했지만 비전 모델 분석에 실패했습니다: %v\n", len(result.Frames), callErr)
	if t := strings.TrimSpace(result.Transcript); t != "" {
		if len(t) > watchMaxTranscriptChars {
			t = t[:watchMaxTranscriptChars] + "\n[자막 일부 생략]"
		}
		fmt.Fprintf(&b, "\n### 자막(%s)\n%s\n", result.Language, t)
	}
	return b.String()
}

// formatWatchDuration renders seconds as "M:SS" or "H:MM:SS".
func formatWatchDuration(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
