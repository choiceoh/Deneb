package externalmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeCall records the last remote tool + args and returns canned output. When
// nodeOut contains codegraphNodeMiss the reroute treats it as a miss.
func fakeCall(nodeOut string) (fn func(context.Context, string, json.RawMessage) (string, error), last *string, lastArgs *json.RawMessage) {
	var name string
	var args json.RawMessage
	f := func(_ context.Context, n string, a json.RawMessage) (string, error) {
		name, args = n, a
		return nodeOut, nil
	}
	return f, &name, &args
}

func exploreInput(q string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"query": q})
	return b
}

func TestTuneCodegraphExploreReroutesSpecificSymbolToNode(t *testing.T) {
	call, lastName, lastArgs := fakeCall("**GatewayHub** (struct)\n\n**Members (25):**\n")
	base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
	fn := tuneCodegraphTool(codegraphExploreTool, base, call)

	out, err := fn(context.Background(), exploreInput("GatewayHub"))
	if err != nil {
		t.Fatalf("reroute err: %v", err)
	}
	if *lastName != codegraphNodeTool {
		t.Fatalf("expected reroute to %s, got %q", codegraphNodeTool, *lastName)
	}
	var na struct {
		Symbol      string `json:"symbol"`
		IncludeCode bool   `json:"includeCode"`
	}
	if err := json.Unmarshal(*lastArgs, &na); err != nil || na.Symbol != "GatewayHub" || !na.IncludeCode {
		t.Fatalf("node args = %s (want symbol=GatewayHub includeCode=true)", *lastArgs)
	}
	if strings.Contains(out, "EXPLORE_FALLBACK") {
		t.Fatal("reroute must not fall through to explore on a node hit")
	}
	if !strings.Contains(out, "GatewayHub") || !strings.Contains(out, "단일 심볼") {
		t.Fatalf("reroute output missing node body or note: %q", out)
	}
}

func TestTuneCodegraphExploreFallsBackOnNodeMiss(t *testing.T) {
	call, lastName, _ := fakeCall(`Symbol "GatewayHub" not found in the codebase`)
	base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
	fn := tuneCodegraphTool(codegraphExploreTool, base, call)

	out, err := fn(context.Background(), exploreInput("Zzznotasymbol"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// node was tried (it's a specific-looking symbol) but missed → explore wins.
	if *lastName != codegraphNodeTool {
		t.Fatalf("expected node attempt, got %q", *lastName)
	}
	if out != "EXPLORE_FALLBACK" {
		t.Fatalf("node miss must fall back to explore, got %q", out)
	}
}

func TestTuneCodegraphExplorePassesThroughNonSymbolQueries(t *testing.T) {
	cases := map[string]string{
		"multi-token flow": "where chat turn execution happens",
		"two symbols":      "GatewayHub Broadcaster",
		"lowercase area":   "auth", // names a symbol maybe, but area intent → keep explore
		"dotted method":    "miniapp.people.list",
		"too short":        "Go",
	}
	for label, q := range cases {
		t.Run(label, func(t *testing.T) {
			call, lastName, _ := fakeCall("NODE_SHOULD_NOT_RUN")
			base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
			fn := tuneCodegraphTool(codegraphExploreTool, base, call)
			out, err := fn(context.Background(), exploreInput(q))
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if *lastName != "" {
				t.Fatalf("%q must not reroute (node called %q)", q, *lastName)
			}
			if out != "EXPLORE_FALLBACK" {
				t.Fatalf("%q must pass through to explore, got %q", q, out)
			}
		})
	}
}

func TestTuneCodegraphLeavesOtherToolsUntouched(t *testing.T) {
	call, _, _ := fakeCall("x")
	base := func(context.Context, json.RawMessage) (string, error) { return "BASE", nil }
	for _, tool := range []string{"codegraph_node", "codegraph_callers", "codegraph_impact", "codegraph_search"} {
		fn := tuneCodegraphTool(tool, base, call)
		out, _ := fn(context.Background(), exploreInput("GatewayHub"))
		if out != "BASE" {
			t.Fatalf("%s must be passthrough, got %q", tool, out)
		}
	}
}
