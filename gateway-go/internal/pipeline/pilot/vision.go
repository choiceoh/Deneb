// vision.go — isolated multimodal (vision) LLM call for the "watch" tool.
//
// The agent's tool contract is text-only: a tool returns (string, error), so a
// tool cannot inject image blocks into the running conversation mid-turn. To let
// the agent "watch" a video, we extract representative frames + the subtitle
// track and analyze them in a SEPARATE vision call here, returning only the
// resulting analysis text to the conversation — the same isolation pattern
// web_youtube.go uses for transcript summarization (see docs/agent-rules/
// prompt-cache.md §5). The heavy multimodal payload (base64 frames) never enters
// the main transcript, so the prompt cache and context budget stay intact.
//
// The call prefers RoleVision (the operator-assigned multimodal model, e.g. a
// VLM); when that role is unset it falls through RoleMain → fallback → lightweight
// so a deployment whose main model is itself multimodal still works.
package pilot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	for i, frame := range frames {
		if frame.Base64 == "" {
			return "", fmt.Errorf("frame %d has no image data", i)
		}
	}

	client, model := visionClientAndModel()
	if client == nil {
		return "", fmt.Errorf("no vision-capable model client available")
	}

	ctx, cancel := context.WithTimeout(ctx, visionTimeout)
	defer cancel()

	text, err := streamVision(ctx, client, model, system, userText, frames, maxTokens)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("empty response from vision model")
	}
	return text, nil
}

// streamVision assembles the multimodal request (text prompt + inline base64
// image blocks) and returns the collected reply. Timeout is the caller's
// responsibility (it already bounds ctx). Shared by CallVisionLLM and the
// role-pinned attachment describe path.
func streamVision(ctx context.Context, client *llm.Client, model, system, userText string, frames []VisionFrame, maxTokens int) (string, error) {
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
	return CollectStream(ctx, events)
}

// visionDescribeTimeout bounds a single attached-image describe call — one image,
// so much tighter than the video-frame visionTimeout, but still generous for a
// slow VLM. In a batch each image describe runs under this bound independently.
const visionDescribeTimeout = 90 * time.Second

// DescribeImage returns a text understanding of one attached image, trying the
// vision-capable model chain in order — the MAIN model first (the smartest, and
// its read integrates with the analysis that follows) when it accepts images,
// then the dedicated RoleVision model — and returns "" when neither is available
// or every attempt fails/empties. The caller (attachment capture) then falls back
// to OCR. system is the describe instruction; imageBase64 is the raw image.
func DescribeImage(ctx context.Context, system, userText, mimeType, imageBase64 string, maxTokens int) string {
	if imageBase64 == "" {
		return ""
	}
	frames := []VisionFrame{{MimeType: mimeType, Base64: imageBase64}}
	for _, role := range visionDescribeRoles() {
		out, err := callVisionRole(ctx, role, system, userText, frames, maxTokens)
		if err != nil {
			slog.Warn("image describe: role failed", "role", role, "error", err)
			continue
		}
		if out = strings.TrimSpace(out); out != "" {
			return out
		}
	}
	return ""
}

// visionDescribeRoles is the ordered role chain for describing an attached image:
// the main model first (only when it is known to accept images — a model marked
// vision:false in the catalog is skipped so we don't waste a doomed call), then
// the dedicated vision role when configured. An unconfigured role is skipped.
func visionDescribeRoles() []modelrole.Role {
	if pkgRegistry == nil {
		return nil
	}
	var roles []modelrole.Role
	if main := pkgRegistry.Config(modelrole.RoleMain); main.Model != "" {
		if !pkgRegistry.CapabilityForModel(main.ProviderID, main.Model).NoVision {
			roles = append(roles, modelrole.RoleMain)
		}
	}
	if pkgRegistry.Client(modelrole.RoleVision) != nil {
		roles = append(roles, modelrole.RoleVision)
	}
	return roles
}

// callVisionRole issues a vision request to a SPECIFIC role's client with no
// fall-through, so DescribeImage owns the chain order (unlike CallVisionLLM,
// whose visionClientAndModel picks one role). Errors when the role has no client.
func callVisionRole(ctx context.Context, role modelrole.Role, system, userText string, frames []VisionFrame, maxTokens int) (string, error) {
	if pkgRegistry == nil {
		return "", fmt.Errorf("no model registry")
	}
	client := pkgRegistry.Client(role)
	if client == nil {
		return "", fmt.Errorf("role %s has no client", role)
	}
	ctx, cancel := context.WithTimeout(ctx, visionDescribeTimeout)
	defer cancel()
	return streamVision(ctx, client, pkgRegistry.Model(role), system, userText, frames, maxTokens)
}

// visionClientAndModel returns the configured vision model client and its model
// name. Prefers RoleVision (the operator-assigned multimodal model); if that role
// is unset it falls through to main/fallback/lightweight so deployments whose main
// model is itself multimodal keep working.
func visionClientAndModel() (client *llm.Client, model string) {
	if pkgRegistry == nil {
		return nil, ""
	}
	for _, role := range []modelrole.Role{modelrole.RoleVision, modelrole.RoleMain, modelrole.RoleFallback, modelrole.RoleLightweight} {
		if client := pkgRegistry.Client(role); client != nil {
			return client, pkgRegistry.Model(role)
		}
	}
	return nil, ""
}
