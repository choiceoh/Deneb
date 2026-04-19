package prompt

import (
	"encoding/json"
	"os"
	"testing"
)

// TestToolCategoriesMatchRegistry asserts every name in toolCategories exists
// in the tool registry. This prevents phantom (never-registered) names from
// accumulating — the render-time filter silently drops them, so drift is
// invisible without this check.
func TestToolCategoriesMatchRegistry(t *testing.T) {
	data, err := os.ReadFile("../toolreg/tool_schemas.json")
	if err != nil {
		t.Fatalf("read tool_schemas.json: %v", err)
	}
	var schemas []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &schemas); err != nil {
		t.Fatalf("parse tool_schemas.json: %v", err)
	}
	registered := make(map[string]struct{}, len(schemas))
	for _, s := range schemas {
		registered[s.Name] = struct{}{}
	}
	for _, cat := range toolCategories {
		for _, name := range cat.Names {
			if _, ok := registered[name]; !ok {
				t.Errorf("toolCategories[%q] references %q which is not in tool_schemas.json", cat.Label, name)
			}
		}
	}
}
