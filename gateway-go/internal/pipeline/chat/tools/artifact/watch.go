// watch.go — the video tool and the single YouTube entry point.
//
// This is the ONE tool for YouTube (the web tool steers YouTube URLs here). Two
// modes, via the `detail` param:
//
//   - detail=transcript (DEFAULT): captions/ASR only — delegates to the shared
//     YouTube transcript engine (web.FetchYouTube: metadata + a detailed,
//     chapter-sectioned summary, chunked for long talks). Light and fast: no
//     video download. This is what "review this video" wants.
//   - detail=frames (opt-in): also extracts representative frames and analyzes
//     them with the main multimodal model in an ISOLATED vision call
//     (pilot.CallVisionLLM) so the agent actually SEES the screen. Only the
//     analysis text flows back — the base64 frames never touch the main
//     transcript, preserving the prompt cache and context budget.
//
// Reach for frames only when the visuals matter (benchmarks on screen, a UI/demo,
// a screen recording, diagnosing a bug). For a talking-head review the transcript
// default is enough. Local video files use the native frames/transcript path.
package artifact

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

type watchParams struct {
	Source string  `json:"source"`           // YouTube URL or local video file path
	Task   string  `json:"task,omitempty"`   // what to analyze (default: general analysis)
	Start  float64 `json:"start,omitempty"`  // optional window start (seconds)
	End    float64 `json:"end,omitempty"`    // optional window end (seconds)
	Detail string  `json:"detail,omitempty"` // "transcript" (default) | "frames"
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

// YouTubeFetcher fetches + summarizes a YouTube URL's transcript. Injected so
// the tools layer needn't import the web backend (wiring lives in toolwire); a
// nil fetcher falls back to the native yt-dlp caption path.
type YouTubeFetcher func(ctx context.Context, url string) (string, error)

// ToolWatch returns the video/YouTube tool. Default (detail=transcript) returns
// a caption summary via fetchYouTube for YouTube URLs; detail=frames extracts
// frames + runs vision. workspaceDir bounds local-file access (empty = URL-only);
// fetchYouTube may be nil (then YouTube transcript uses the native path).
func ToolWatch(workspaceDir string, fetchYouTube YouTubeFetcher) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p watchParams
		if err := jsonutil.UnmarshalInto("watch params", input, &p); err != nil {
			return "", err
		}
		p.Source = strings.TrimSpace(p.Source)
		if p.Source == "" {
			return "", fmt.Errorf("source는 필수입니다 (유튜브 URL 또는 영상 파일 경로)")
		}
		detail, err := normalizeWatchDetail(p.Detail)
		if err != nil {
			return "", err
		}
		p.Detail = detail

		// YouTube + transcript (the default) → the shared transcript engine
		// (metadata + a detailed, chapter-sectioned summary, chunked for long
		// talks), injected via fetchYouTube. This is the single YouTube path now
		// (the web tool steers here). Frames mode, local files, a nil fetcher,
		// and windowed (start/end) requests fall through to the native path
		// below — the fetcher summarizes the WHOLE video and cannot honor a
		// window, while WatchVideo transcribes just that window's audio.
		windowed := p.Start > 0 || p.End > 0
		if fetchYouTube != nil && detail == watchDetailTranscript && !windowed && media.IsYouTubeURL(p.Source) {
			out, ferr := fetchYouTube(ctx, p.Source)
			if ferr != nil {
				return "", fmt.Errorf("유튜브 자막 처리 실패: %w", ferr)
			}
			return out, nil
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
			StartSec:       p.Start,
			EndSec:         p.End,
			TranscriptOnly: detail == watchDetailTranscript,
		})
		if err != nil {
			return "", fmt.Errorf("영상 처리 실패: %w", err)
		}

		// Transcript-only: skip vision entirely — captions/ASR already collected.
		if detail == watchDetailTranscript {
			if textAnalysis := summarizeWatchTranscript(ctx, &p, result); textAnalysis != "" {
				return formatWatchTextResult(result, textAnalysis), nil
			}
			if strings.TrimSpace(result.Transcript) == "" {
				return formatWatchNoTranscript(result), nil
			}
			return "", fmt.Errorf("자막/전사 분석에 실패했습니다")
		}

		if len(result.Frames) == 0 {
			if textAnalysis := summarizeWatchTranscript(ctx, &p, result); textAnalysis != "" {
				return formatWatchTextResult(result, textAnalysis), nil
			}
			return "", fmt.Errorf("영상에서 프레임을 추출하지 못했습니다 (ffmpeg 설치 여부 확인)")
		}

		analysis, err := analyzeWatch(ctx, &p, result)
		if err != nil {
			// Vision unavailable (e.g., the main model is text-only and rejects
			// image blocks) — don't dead-end. Summarize the transcript (captions
			// or ASR) with a text model so the user still gets a real analysis.
			if textAnalysis := summarizeWatchTranscript(ctx, &p, result); textAnalysis != "" {
				return formatWatchTextResult(result, textAnalysis), nil
			}
			// No transcript either — return metadata + whatever we have.
			return formatWatchFallback(result, err), nil
		}
		return formatWatchResult(result, analysis), nil
	}
}

const (
	watchDetailFrames     = "frames"
	watchDetailTranscript = "transcript"
)

func normalizeWatchDetail(detail string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(detail)) {
	case "", watchDetailTranscript:
		// Default is transcript (light: captions only, no video download).
		// Seeing/hearing the frames is opt-in via detail=frames.
		return watchDetailTranscript, nil
	case watchDetailFrames:
		return watchDetailFrames, nil
	default:
		return "", fmt.Errorf("detail은 frames 또는 transcript 여야 합니다 (got %q)", detail)
	}
}

const watchTextSystemPrompt = "당신은 영상의 자막/음성 전사를 바탕으로 영상을 분석하는 전문가입니다. " +
	"화면을 직접 보지는 못했으니 자막/전사 내용만으로 핵심 주제, 주요 논점, 결론을 충실히 정리하세요. " +
	"요청된 작업이 있으면 그에 집중하고, 불필요한 서두 없이 한국어로 분석 결과만 출력하세요."

// summarizeWatchTranscript produces a text-only analysis from the extracted
// transcript when the vision call is unavailable. Returns "" when there is no
// transcript or the text model is unavailable, so the caller can degrade further.
func summarizeWatchTranscript(ctx context.Context, p *watchParams, result *media.WatchResult) string {
	t := strings.TrimSpace(result.Transcript)
	if t == "" {
		return ""
	}
	if len(t) > watchMaxTranscriptChars {
		t = t[:watchMaxTranscriptChars] + "\n[자막 일부 생략]"
	}
	task := strings.TrimSpace(p.Task)
	if task == "" {
		task = "이 영상의 핵심 내용과 구조를 분석해줘."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "작업: %s\n\n", task)
	if result.Title != "" {
		fmt.Fprintf(&b, "영상: %s\n", result.Title)
	}
	if result.Channel != "" {
		fmt.Fprintf(&b, "채널: %s\n", result.Channel)
	}
	b.WriteString(watchChaptersBlock(result.Chapters))
	fmt.Fprintf(&b, "\n자막/전사(%s):\n%s\n", result.Language, t)

	// Free-text transcript analysis on the non-reasoning lightweight model →
	// append the reflective self-check to cut errors/omissions (arXiv:2507.02778).
	out, err := pilot.CallLocalLLM(ctx, watchTextSystemPrompt+"\n"+pilot.ReflectionDirective, b.String(), watchAnalysisMaxTokens)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// formatWatchTextResult renders a transcript-based analysis (vision skipped),
// noting that the screen wasn't analyzed so the reader knows the basis.
func formatWatchTextResult(result *media.WatchResult, analysis string) string {
	var b strings.Builder
	b.WriteString("## 🎬 영상 분석 (자막/음성 기반)\n\n")
	if result.Title != "" {
		fmt.Fprintf(&b, "**%s**", result.Title)
		if result.Channel != "" {
			fmt.Fprintf(&b, " — %s", result.Channel)
		}
		b.WriteString("\n")
	}
	meta := []string{}
	if result.DurationSec > 0 {
		meta = append(meta, formatWatchDuration(result.DurationSec))
	}
	if strings.TrimSpace(result.Language) != "" {
		meta = append(meta, result.Language)
	}
	meta = append(meta, "프레임 분석 미사용")
	fmt.Fprintf(&b, "_%s_\n\n", strings.Join(meta, " · "))
	b.WriteString(strings.TrimSpace(analysis))
	b.WriteString("\n")
	return b.String()
}

func formatWatchNoTranscript(result *media.WatchResult) string {
	var b strings.Builder
	b.WriteString("## 🎬 영상 분석 (자막/음성 기반)\n\n")
	if result.Title != "" {
		fmt.Fprintf(&b, "**%s**", result.Title)
		if result.Channel != "" {
			fmt.Fprintf(&b, " — %s", result.Channel)
		}
		b.WriteString("\n")
	}
	if result.IsLive {
		b.WriteString("라이브라 자막 트랙이 없습니다. 오디오 전사를 시도했지만 받지 못했어요 — 방송이 끝나면 다시 주시거나, 보고 싶은 구간(초)을 지정해 주세요.\n")
		return b.String()
	}
	b.WriteString("자막을 받지 못했어요. 라이브가 아니면 detail=frames로 화면을 보거나, 구간(초)을 지정해 다시 시도해 주세요.\n")
	return b.String()
}

// analyzeWatch runs the isolated vision call over the extracted frames.
func analyzeWatch(ctx context.Context, p *watchParams, result *media.WatchResult) (string, error) {
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
// watchChaptersBlock renders a compact chapter list for the analysis prompt so
// the model can structure its analysis by section. Empty when no chapters.
func watchChaptersBlock(chapters []media.YouTubeChapter) string {
	if len(chapters) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n챕터:\n")
	for _, c := range chapters {
		fmt.Fprintf(&b, "- %s %s\n", formatWatchDuration(c.StartSec), c.Title)
	}
	return b.String()
}

func buildWatchPrompt(p *watchParams, result *media.WatchResult) string {
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
	b.WriteString(watchChaptersBlock(result.Chapters))

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
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
