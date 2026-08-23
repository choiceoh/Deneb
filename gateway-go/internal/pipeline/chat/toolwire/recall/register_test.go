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

func TestRegisterKnowledgeToolPublishesFactAuthorityBoundary(t *testing.T) {
	registry := &knowledgeRegistrationCapture{}
	RegisterKnowledgeTool(registry, knowledge.New(&knowledgeRegistrationAdapter{}))
	if registry.def.Name != "knowledge" || registry.def.Fn == nil {
		t.Fatalf("registered definition = %+v", registry.def)
	}
	for _, want := range []string{"assert_fact", "forget_fact", "facts", "direct_user", "금지", "최대 50건"} {
		if !strings.Contains(registry.def.Description, want) {
			t.Errorf("description missing %q: %s", want, registry.def.Description)
		}
	}

	properties, ok := registry.def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", registry.def.InputSchema["properties"])
	}
	op := schemaProperty(t, properties, "op")
	for _, want := range []string{"assert_fact", "forget_fact", "facts"} {
		if !containsSchemaString(op["enum"], want) {
			t.Errorf("op enum missing %q: %#v", want, op["enum"])
		}
	}
	authority := schemaProperty(t, properties, "authority")
	if containsSchemaString(authority["enum"], "direct_user") {
		t.Fatalf("model-callable authority enum exposes direct_user: %#v", authority["enum"])
	}
	for _, want := range []string{"primary_document", "runtime_observation", "agent_confirmed", "inference"} {
		if !containsSchemaString(authority["enum"], want) {
			t.Errorf("authority enum missing %q: %#v", want, authority["enum"])
		}
	}
	if authority["default"] != "agent_confirmed" {
		t.Errorf("authority default = %#v", authority["default"])
	}
	if got := schemaProperty(t, properties, "subject")["default"]; got != "self" {
		t.Errorf("subject default = %#v", got)
	}
	if got := schemaProperty(t, properties, "fact_kind")["default"]; got != "generic" {
		t.Errorf("fact_kind default = %#v", got)
	}
	limit := schemaProperty(t, properties, "limit")
	if got := limit["maximum"]; got != 50 {
		t.Errorf("facts limit maximum = %#v", got)
	}
	if description, _ := limit["description"].(string); !strings.Contains(description, "facts 이력은 최신 N개") {
		t.Errorf("limit description does not document bounded fact history: %q", description)
	}
	sourceRefs := schemaProperty(t, properties, "source_refs")
	if got := sourceRefs["maxItems"]; got != 16 {
		t.Errorf("source_refs maxItems = %#v", got)
	}
	if description, _ := sourceRefs["description"].(string); !strings.Contains(description, "필수") {
		t.Errorf("source_refs description does not document authority requirement: %q", description)
	}
	if description, _ := schemaProperty(t, properties, "basis_at")["description"].(string); !strings.Contains(description, "필수") {
		t.Errorf("basis_at description does not document dated-primary requirement: %q", description)
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
