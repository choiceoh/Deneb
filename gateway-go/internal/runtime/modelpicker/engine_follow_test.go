package modelpicker

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginecontrol"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

func boolPtr(v bool) *bool { return &v }

// The router's entries at the engine: GLM-5.3 and Qwen3.8, each thinking on
// and off; Qwen3.8's text-only.
var followEntries = []enginecontrol.Entry{
	{Name: "glm-5.3-flash", UpstreamModel: "glm-5.3-flash", ThinkingMode: "on", Vision: boolPtr(true)},
	{Name: "glm-5.3-flash-low", UpstreamModel: "glm-5.3-flash", ThinkingMode: "off", Vision: boolPtr(true)},
	{Name: "qwen3.8-flash-next", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "on", Vision: boolPtr(false)},
	{Name: "qwen3.8-flash-next-low", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "off", Vision: boolPtr(false)},
}

// followFixture is a picker over a throwaway deneb.json — never the host's —
// whose roles sit on the engine's GLM-5.3 entries. offered lists the wormhole
// models the picker may bind.
func followFixture(t *testing.T, offered ...string) (*Controller, string) {
	t.Helper()
	models := make([]map[string]string, 0, len(offered))
	for _, id := range offered {
		models = append(models, map[string]string{"id": id})
	}
	cfg := map[string]any{
		"agents": map[string]any{
			"defaultModel": "kimi/k3", "codingModel": "wormhole/glm-5.3-flash", "lightweightModel": "wormhole/glm-5.3-flash",
			"tinyModel": "wormhole/glm-5.3-flash-low", "visionModel": "wormhole/glm-5.3-flash",
		},
		// An unroutable loopback port: discovery fails at once and never dials a live router.
		"models": map[string]any{"providers": map[string]any{
			"wormhole": map[string]any{"baseUrl": "http://127.0.0.1:1/v1", "api": "openai-chat", "models": models},
		}},
	}
	raw, _ := json.Marshal(cfg)
	path := filepath.Join(t.TempDir(), "deneb.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_CONFIG_PATH", path)
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "kimi/k3", CodingModel: "wormhole/glm-5.3-flash", LightweightModel: "wormhole/glm-5.3-flash",
		TinyModel: "wormhole/glm-5.3-flash-low", VisionModel: "wormhole/glm-5.3-flash",
		FallbackModel: "wormhole/deepseek-v4-flash-api",
		// Registry construction probes vLLM-backed providers; unroutable, so it never dials a live one.
		Providers: map[string]modelrole.ProviderResolved{
			"wormhole": {BaseURL: "http://127.0.0.1:1/v1"},
			"vllm":     {BaseURL: "http://127.0.0.1:1/v1"},
		},
	})
	return NewController(ControllerConfig{Registry: reg, Logger: slog.Default()}), path
}

// The engine now serves Qwen3.8: the local roles move by variant, through the
// picker's persist-and-apply path, and vision stays off a text-only door.
func TestFollowEngineMovesTheLocalRolesThroughThePicker(t *testing.T) {
	ctrl, path := followFixture(t, "glm-5.3-flash", "glm-5.3-flash-low", "qwen3.8-flash-next", "qwen3.8-flash-next-low")

	moves, skips := ctrl.FollowEngine(context.Background(), followEntries, "qwen3.8-flash-next")
	got := map[string]string{}
	for _, m := range moves {
		got[m.Role] = m.To
	}
	want := map[string]string{
		"coding": "wormhole/qwen3.8-flash-next", "lightweight": "wormhole/qwen3.8-flash-next",
		"tiny": "wormhole/qwen3.8-flash-next-low",
	}
	for role, to := range want {
		if got[role] != to {
			t.Errorf("%s moved to %q, want %q (moves %+v, skips %+v)", role, got[role], to, moves, skips)
		}
	}
	if len(skips) != 1 || skips[0].Role != "vision" {
		t.Fatalf("skips = %+v, want vision alone held back", skips)
	}

	// Live and on disk, the way a tap in the picker leaves them.
	if id := ctrl.modelRegistry.FullModelID(modelrole.RoleCoding); id != "wormhole/qwen3.8-flash-next" {
		t.Errorf("registry coding = %q", id)
	}
	var onDisk struct {
		Agents map[string]string `json:"agents"`
	}
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Agents["tinyModel"] != "wormhole/qwen3.8-flash-next-low" || onDisk.Agents["visionModel"] != "wormhole/glm-5.3-flash" {
		t.Errorf("deneb.json agents = %+v", onDisk.Agents)
	}
}

// A model the picker does not offer is not bound behind its back: the role
// stays, and the reason comes back as a skip.
func TestFollowEngineLeavesARoleThePickerRefuses(t *testing.T) {
	ctrl, _ := followFixture(t, "glm-5.3-flash", "glm-5.3-flash-low") // the Qwen3.8 entries not offered
	moves, skips := ctrl.FollowEngine(context.Background(), followEntries, "qwen3.8-flash-next")
	if len(moves) != 0 {
		t.Fatalf("moves = %+v, want none", moves)
	}
	if len(skips) != 4 {
		t.Fatalf("skips = %+v, want coding/lightweight/tiny refused and vision held back", skips)
	}
	if id := ctrl.modelRegistry.FullModelID(modelrole.RoleCoding); id != "wormhole/glm-5.3-flash" {
		t.Errorf("coding = %q, want it untouched", id)
	}
}
