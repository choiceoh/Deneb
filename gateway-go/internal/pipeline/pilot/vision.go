// vision.go — isolated multimodal (vision) LLM call for the "watch" tool.
//
// The agent's tool contract is text-only: a tool returns (string, error), so a
// tool cannot inject image blocks into the running conversation mid-turn. To let
// the agent "watch" a video, we extract representative frames + the subtitle
// track and analyze them in a SEPARATE vision call here, returning only the
// resulting analysis text to the conversation — the same isolation pattern
// web_youtube.go uses for transcript summarization (see .claude/rules/
// prompt-cache.md §5). The heavy multimodal payload (base64 frames) never enters
// the main transcript, so the prompt cache and context budget stay intact.
//
// The call targets RoleMain because that is the configured multimodal model;
// the lightweight local model is typically text-only. We fall back through the
// role chain if the main client is unavailable.
package pilot

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// VisionFrame is a single JPEG/PNG frame to feed the vision model.
type VisionFrame struct {
	MimeType string // e.g. "image/jpeg"
	Base64   string // base64-encoded image bytes
}

// visionTimeout bounds a single watch analysis call. Frame analysis over the
// main model is slower than a text summary, so this is more generous than
// pilotTimeout but still bounded so a stuck call cannot hold the turn deadline.
const visionTimeout = 3 * time.Minute

// CallVisionLLM analyzes the given frames (and optional accompanying text such
// as a subtitle transcript) with the main multimodal model and returns the
// analysis text. The frames are sent as inline base64 image blocks in a single
// user message, preceded by the text prompt.
//
// system is the analysis instruction; userText is the per-call prompt (task +
// any transcript); frames are the images. maxTokens caps the generated answer.
func CallVisionLLM(ctx context.Context, system, userText string, frames []VisionFrame, maxTokens int) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames to analyze")
	}

	client, model := visionClientAndModel()
	if client == nil {
		return "", fmt.Errorf("no vision-capable model client available")
	}

	ctx, cancel := context.WithTimeout(ctx, visionTimeout)
	defer cancel()

	blocks := make([]llm.ContentBlock, 0, len(frames)+1)
	if userText != "" {
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: userText})
	}
	for _, f := range frames {
		mt := f.MimeType
		if mt == "" {
			mt = "image/jpeg"
		}
		blocks = append(blocks, llm.ContentBlock{
			Type: "image",
			Source: &llm.ImageSource{
				Type:      "base64",
				MediaType: mt,
				Data:      f.Base64,
			},
		})
	}

	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewBlockMessage("user", blocks)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("vision stream: %w", err)
	}

	text, err := CollectStream(ctx, events)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("empty response from vision model")
	}
	return text, nil
}

// visionClientAndModel returns the main (multimodal) model client and its model
// name, falling back through the role chain when the main role has no client.
func visionClientAndModel() (client *llm.Client, model string) {
	if pkgRegistry == nil {
		return nil, ""
	}
	for _, role := range []modelrole.Role{modelrole.RoleMain, modelrole.RoleFallback, modelrole.RoleLightweight} {
		if client := pkgRegistry.Client(role); client != nil {
			return client, pkgRegistry.Model(role)
		}
	}
	return nil, ""
}
