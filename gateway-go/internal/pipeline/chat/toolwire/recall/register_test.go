package recall

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

type knowledgeRegistrationAdapter struct{}

func (*knowledgeRegistrationAdapter) Layer() knowledge.Layer { return knowledge.LayerWiki }

func (*knowledgeRegistrationAdapter) Recall(context.Context, string, int) ([]knowledge.Result, error) {
	return nil, nil
}

func (*knowledgeRegistrationAdapter) Read(context.Context, string) (*knowledge.Document, error) {
	return nil, nil
}

type knowledgeRegistrationCapture struct {
	def toolport.ToolDef
}

func (r *knowledgeRegistrationCapture) RegisterTool(def toolport.ToolDef) { r.def = def }

func TestRegisterKnowledgeToolPublishesReadOnlyFactBoundary(t *testing.T) {
	registry := &knowledgeRegistrationCapture{}
	RegisterKnowledgeTool(registry, knowledge.New(&knowledgeRegistrationAdapter{}))
	if registry.def.Name != "knowledge" || registry.def.Fn == nil {
		t.Fatalf("registered definition = %+v", registry.def)
	}
	for _, want := range []string{"facts", "직접 사용자 발화", "내부 ingestion", "최대 50건"} {
		if !strings.Contains(registry.def.Description, want) {
			t.Errorf("description missing %q: %s", want, registry.def.Description)
		}
	}

	properties, ok := registry.def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", registry.def.InputSchema["properties"])
	}
	op := schemaProperty(t, properties, "op")
	for _, want := range []string{"recall", "read", "record", "facts"} {
		if !containsSchemaString(op["enum"], want) {
			t.Errorf("op enum missing %q: %#v", want, op["enum"])
		}
	}
	for _, forbidden := range []string{"assert_fact", "forget_fact"} {
		if containsSchemaString(op["enum"], forbidden) {
			t.Fatalf("model-callable op enum exposes mutation %q: %#v", forbidden, op["enum"])
		}
	}
	if got := schemaProperty(t, properties, "subject")["default"]; got != "self" {
		t.Errorf("subject default = %#v", got)
	}
	limit := schemaProperty(t, properties, "limit")
	if got := limit["maximum"]; got != 50 {
		t.Errorf("facts limit maximum = %#v", got)
	}
	if description, _ := limit["description"].(string); !strings.Contains(description, "facts 이력은 최신 N개") {
		t.Errorf("limit description does not document bounded fact history: %q", description)
	}
	for _, forbidden := range []string{"value", "fact_kind", "authority", "source_refs", "basis_at", "reason"} {
		if _, exposed := properties[forbidden]; exposed {
			t.Fatalf("model schema exposes fact mutation field %q", forbidden)
		}
	}
}

func schemaProperty(t *testing.T, properties map[string]any, name string) map[string]any {
	t.Helper()
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("property %q = %#v", name, properties[name])
	}
	return property
}

func containsSchemaString(raw any, want string) bool {
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	case []any:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	}
	return false
}
