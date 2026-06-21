package gmailpoll

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// ExtractForEval runs a PRODUCTION extractor — its real system+user prompt, the
// real local-LLM JSON call (jsonutil-parsed, so markdown code fences and thinking
// tags are stripped exactly as prod does), and the real post-processing — against
// an arbitrary model. It exists so a benchmark can score the REAL extraction path
// instead of a raw model probe: point client at any OpenAI-compatible endpoint
// (e.g. the wormhole router) and each of glm/qwen/dsv4/mimo runs the exact
// deal/facts/actions task Deneb runs on every analyzed mail.
//
// The returned value is what prod actually consumes for that kind:
//   - "deal"    → *DealInfo (nil = "not a deal", a valid outcome)
//   - "facts"   → string (the markdown fact block appended to the wiki, "" = none)
//   - "actions" → []ActionItem (empty = "nothing the operator must do")
//
// Because parsing goes through callLocalLLMJSON (jsonutil), a model that wraps its
// JSON in ```json fences still extracts correctly — the benchmark measures the
// consumed result, not raw formatting.
func ExtractForEval(ctx context.Context, client *llm.Client, model, kind, input string) (any, error) {
	if client == nil || model == "" {
		return nil, fmt.Errorf("eval extract: nil client or empty model")
	}
	deps := PipelineDeps{LocalClient: client, LocalModel: model}
	switch kind {
	case "deal":
		return extractDealInfo(ctx, deps, input), nil
	case "facts":
		return extractFactsForWiki(ctx, deps, input), nil
	case "actions":
		return extractActionItems(ctx, deps, input), nil
	default:
		return nil, fmt.Errorf("eval extract: unknown kind %q (want deal|facts|actions)", kind)
	}
}
