package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- YouTube transcript tool ---

func ToolYouTubeTranscript() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL string `json:"url"`
		}
		if err := jsonutil.UnmarshalInto("youtube_transcript params", input, &p); err != nil {
			return "", err
		}
		if p.URL == "" {
			return "", fmt.Errorf("url is required")
		}
		if !media.IsYouTubeURL(p.URL) {
			return "", fmt.Errorf("not a valid YouTube URL: %s", p.URL)
		}

		ytCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		result, err := media.ExtractYouTubeTranscript(ytCtx, p.URL)
		if err != nil {
			return "", fmt.Errorf("youtube transcript extraction failed: %w", err)
		}

		return media.FormatYouTubeResult(result), nil
	}
}
