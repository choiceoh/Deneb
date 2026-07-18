package toolwire

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/chrono"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/core"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

type schemaCase struct {
	name  string
	build func() map[string]any
}

func allSchemaCases() []schemaCase {
	return []schemaCase{
		{name: "read", build: schema.ReadToolSchema},
		{name: "research_panel", build: schema.ResearchPanelToolSchema},
		{name: "read_spillover", build: schema.ReadSpilloverToolSchema},
		{name: "write", build: schema.WriteToolSchema},
		{name: "edit", build: schema.EditToolSchema},
		{name: "grep", build: schema.GrepToolSchema},
		{name: "exec", build: schema.ExecToolSchema},
		{name: "process", build: schema.ProcessToolSchema},
		{name: "web", build: schema.WebToolSchema},
		{name: "cron", build: schema.CronToolSchema},
		{name: "morning_letter", build: schema.MorningLetterToolSchema},
		{name: "evening_letter", build: schema.EveningLetterToolSchema},
		{name: "message", build: schema.MessageToolSchema},
		{name: "gateway", build: schema.GatewayToolSchema},
		{name: "sessions", build: schema.SessionsToolSchema},
		{name: "sessions_spawn", build: schema.SessionsSpawnToolSchema},
		{name: "subagents", build: schema.SubagentsToolSchema},
		{name: "send_file", build: schema.SendFileToolSchema},
		{name: "chart", build: schema.ChartToolSchema},
		{name: "diagram", build: schema.DiagramToolSchema},
		{name: "files", build: schema.FilesToolSchema},
		{name: "skills", build: schema.SkillsToolSchema},
		{name: "deal_ledger", build: schema.DealLedgerToolSchema},
		{name: "wiki", build: schema.WikiToolSchema},
		{name: "wiki_forget", build: schema.WikiForgetToolSchema},
		{name: "preference", build: schema.PreferenceToolSchema},
		{name: "notebook", build: schema.NotebookToolSchema},
		{name: "contacts", build: schema.ContactsToolSchema},
		{name: "calendar", build: schema.CalendarToolSchema},
		{name: "polaris", build: schema.PolarisToolSchema},
		{name: "knowledge", build: schema.KnowledgeToolSchema},
		{name: "fetch_tools", build: schema.FetchToolsToolSchema},
		{name: "graphify", build: schema.GraphifyToolSchema},
		{name: "office", build: schema.OfficeToolSchema},
		{name: "heartbeat_update", build: schema.HeartbeatUpdateToolSchema},
		{name: "todo", build: schema.TodoToolSchema},
		{name: "watch", build: schema.WatchToolSchema},
		{name: "observe", build: schema.ObserveToolSchema},
		{name: "fleet", build: schema.FleetToolSchema},
		{name: "browser", build: schema.BrowserToolSchema},
		{name: "groupware", build: schema.GroupwareToolSchema},
		{name: "phone_read", build: schema.PhoneReadToolSchema},
		{name: "phone_write", build: schema.PhoneWriteToolSchema},
		{name: "workfeed", build: schema.WorkfeedToolSchema},
		{name: "transcribe", build: schema.TranscribeToolSchema},
		{name: "ocr", build: schema.OcrToolSchema},
		{name: "market", build: schema.MarketToolSchema},
		{name: "org", build: schema.OrgToolSchema},
		{name: "goal", build: schema.GoalToolSchema},
		{name: "mail_archive", build: schema.MailArchiveToolSchema},
	}
}

func TestEveryGeneratedToolSchemaRoundTripsAsJSONObject(t *testing.T) {
	cases := allSchemaCases()
	if len(cases) < 40 {
		t.Fatalf("schema inventory unexpectedly small: %d", len(cases))
	}
	seen := make(map[string]bool, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if seen[tc.name] {
				t.Fatalf("duplicate schema inventory name %q", tc.name)
			}
			seen[tc.name] = true
			schema := tc.build()
			if schema == nil {
				t.Fatal("schema is nil")
			}
			if got := schema["type"]; got != "object" {
				t.Fatalf("root type = %#v, want object", got)
			}
			if raw, exists := schema["properties"]; exists {
				properties, ok := raw.(map[string]any)
				if !ok || properties == nil {
					t.Fatalf("properties = %#v, want map", raw)
				}
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("schema JSON marshal: %v", err)
			}
			var roundTrip map[string]any
			if err := json.Unmarshal(data, &roundTrip); err != nil {
				t.Fatalf("schema JSON round-trip: %v", err)
			}
			if roundTrip["type"] != "object" {
				t.Fatalf("round-trip root type = %#v", roundTrip["type"])
			}
		})
	}
}

func TestGeneratedSchemasSatisfyRecursiveStructuralContract(t *testing.T) {
	for _, tc := range allSchemaCases() {
		t.Run(tc.name, func(t *testing.T) {
			validateSchemaNode(t, tc.name, tc.build())
		})
	}
}

func validateSchemaNode(t *testing.T, path string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if rawType, ok := value["type"]; ok {
			if typeName, ok := rawType.(string); ok {
				switch typeName {
				case "object", "array", "string", "integer", "number", "boolean", "null":
				default:
					t.Errorf("%s has unsupported type %q", path, typeName)
				}
			}
		}
		if propertiesRaw, ok := value["properties"]; ok {
			properties, ok := propertiesRaw.(map[string]any)
			if !ok {
				t.Errorf("%s properties has type %T", path, propertiesRaw)
			} else {
				for name, child := range properties {
					if strings.TrimSpace(name) == "" {
						t.Errorf("%s has blank property name", path)
					}
					validateSchemaNode(t, path+".properties."+name, child)
				}
				validateRequired(t, path, value["required"], properties)
			}
		} else if required := stringSlice(value["required"]); len(required) > 0 {
			t.Errorf("%s declares required=%v without properties", path, required)
		}
		if value["type"] == "array" {
			if _, ok := value["items"]; !ok {
				t.Errorf("%s array has no items schema", path)
			}
		}
		if enumRaw, ok := value["enum"]; ok {
			validateEnum(t, path, enumRaw)
		}
		for _, key := range []string{"items", "additionalProperties", "anyOf", "oneOf", "allOf", "$defs", "definitions"} {
			if child, ok := value[key]; ok {
				validateSchemaNode(t, path+"."+key, child)
			}
		}
	case []any:
		for i, child := range value {
			validateSchemaNode(t, fmt.Sprintf("%s[%d]", path, i), child)
		}
	case []map[string]any:
		for i, child := range value {
			validateSchemaNode(t, fmt.Sprintf("%s[%d]", path, i), child)
		}
	case bool, string, float64, int, int64, nil:
		// Scalar schema metadata.
	default:
		// Generated schemas use []string for required/enum in a few places.
		rv := reflect.ValueOf(node)
		if rv.IsValid() && rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				validateSchemaNode(t, fmt.Sprintf("%s[%d]", path, i), rv.Index(i).Interface())
			}
			return
		}
		t.Errorf("%s contains unsupported schema value %T", path, node)
	}
}

func validateRequired(t *testing.T, path string, raw any, properties map[string]any) {
	t.Helper()
	required := stringSlice(raw)
	seen := make(map[string]bool, len(required))
	for _, name := range required {
		if strings.TrimSpace(name) == "" {
			t.Errorf("%s has blank required name", path)
		}
		if seen[name] {
			t.Errorf("%s repeats required property %q", path, name)
		}
		seen[name] = true
		if _, ok := properties[name]; !ok {
			t.Errorf("%s requires absent property %q", path, name)
		}
	}
}

func validateEnum(t *testing.T, path string, raw any) {
	t.Helper()
	values := interfaceSlice(raw)
	if len(values) == 0 {
		t.Errorf("%s enum is empty", path)
		return
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		key := fmt.Sprintf("%T:%v", value, value)
		if seen[key] {
			t.Errorf("%s enum repeats %#v", path, value)
		}
		seen[key] = true
	}
}

func stringSlice(raw any) []string {
	switch values := raw.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func interfaceSlice(raw any) []any {
	switch values := raw.(type) {
	case []any:
		return values
	case []string:
		result := make([]any, len(values))
		for i, value := range values {
			result[i] = value
		}
		return result
	case []int:
		result := make([]any, len(values))
		for i, value := range values {
			result[i] = value
		}
		return result
	default:
		return nil
	}
}

func TestRequiredFieldsMatchMinimalSchemaContract(t *testing.T) {
	want := map[string][]string{
		"read":             {"file_path"},
		"write":            {"file_path", "content"},
		"edit":             {"file_path", "new_string"},
		"grep":             {"pattern"},
		"exec":             {"command"},
		"sessions_spawn":   {"task"},
		"send_file":        {"file_path"},
		"fetch_tools":      nil,
		"heartbeat_update": {"content"},
		"phone_read":       {"what"},
		"phone_write":      {"to"},
		"transcribe":       {"path"},
		"ocr":              {"path"},
		"mail_archive":     {"action"},
	}
	byName := make(map[string]schemaCase)
	for _, tc := range allSchemaCases() {
		byName[tc.name] = tc
	}
	for name, required := range want {
		t.Run(name, func(t *testing.T) {
			tc, ok := byName[name]
			if !ok {
				t.Fatalf("schema %q absent from inventory", name)
			}
			got := stringSlice(tc.build()["required"])
			if !reflect.DeepEqual(got, required) {
				t.Fatalf("required = %#v, want %#v", got, required)
			}
		})
	}
}

func TestActionEnumsAreNonEmptyUniqueAndDocumented(t *testing.T) {
	actionSchemas := []string{
		"process", "cron", "message", "gateway", "sessions", "subagents",
		"files", "skills", "wiki", "notebook", "contacts", "calendar", "polaris",
		"observe", "fleet", "browser", "groupware", "workfeed", "goal", "mail_archive",
	}
	byName := make(map[string]schemaCase)
	for _, tc := range allSchemaCases() {
		byName[tc.name] = tc
	}
	for _, name := range actionSchemas {
		t.Run(name, func(t *testing.T) {
			properties := byName[name].build()["properties"].(map[string]any)
			action, ok := properties["action"].(map[string]any)
			if !ok {
				t.Fatalf("action property = %#v", properties["action"])
			}
			values := interfaceSlice(action["enum"])
			if len(values) == 0 {
				t.Fatalf("action enum = %#v", action["enum"])
			}
			seen := make(map[string]bool, len(values))
			for _, raw := range values {
				value, ok := raw.(string)
				if !ok || strings.TrimSpace(value) == "" || seen[value] {
					t.Errorf("invalid action enum value %#v", raw)
				}
				seen[value] = true
			}
			if desc, _ := action["description"].(string); strings.TrimSpace(desc) == "" {
				t.Errorf("action description is empty")
			}
		})
	}
}

func TestSchemaBuildersReturnFreshMutableGraphs(t *testing.T) {
	for _, tc := range allSchemaCases() {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.build()
			second := tc.build()
			first["type"] = "mutated"
			if firstProps, ok := first["properties"].(map[string]any); ok {
				firstProps["__injected"] = map[string]any{"type": "string"}
			}
			if second["type"] != "object" {
				t.Fatalf("root map reused between calls")
			}
			if secondProps, ok := second["properties"].(map[string]any); ok {
				if _, exists := secondProps["__injected"]; exists {
					t.Fatalf("properties map reused between calls")
				}
			}
		})
	}
}

func TestFetchToolsSchemaMatchesGeneratedBuilderWithoutAliasing(t *testing.T) {
	public := FetchToolsSchema()
	direct := schema.FetchToolsToolSchema()
	if !reflect.DeepEqual(public, direct) {
		t.Fatalf("public fetch schema differs from generated schema")
	}
	public["type"] = "mutated"
	if got := FetchToolsSchema()["type"]; got != "object" {
		t.Fatalf("public schema reused map: %#v", got)
	}
}

func TestToolMaxOutputsContractAndFreshMap(t *testing.T) {
	want := map[string]int{
		"browser":     32000,
		"calendar":    8000,
		"contacts":    8000,
		"deal_ledger": 8000,
		"exec":        32000,
		"groupware":   32000,
		"notebook":    24000,
		"office":      32000,
		"wiki":        20000,
	}
	got := ToolMaxOutputs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("max outputs = %#v, want %#v", got, want)
	}
	for name, budget := range got {
		if budget <= 0 || budget > 100_000 {
			t.Errorf("tool %q budget = %d", name, budget)
		}
	}
	got["exec"] = 1
	got["injected"] = 1
	fresh := ToolMaxOutputs()
	if fresh["exec"] != 32000 {
		t.Fatalf("max output map reused: %#v", fresh)
	}
	if _, exists := fresh["injected"]; exists {
		t.Fatalf("injected max output persisted")
	}
}

func TestRegisterCoreToolsWithMinimalDependenciesHasValidUniqueContracts(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &tooldeps.CoreToolDeps{WorkspaceDir: t.TempDir()}
	RegisterCoreTools(reg, deps)
	if len(reg.tools) < 25 {
		t.Fatalf("minimal core registered only %d tools: %v", len(reg.tools), reg.toolNames())
	}
	seen := make(map[string]bool, len(reg.tools))
	for _, def := range reg.tools {
		if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.Description) == "" {
			t.Errorf("blank tool identity: %#v", def)
		}
		if seen[def.Name] {
			t.Errorf("duplicate registered tool %q", def.Name)
		}
		seen[def.Name] = true
		if def.Fn == nil {
			t.Errorf("tool %q has nil implementation", def.Name)
		}
		if def.InputSchema == nil || def.InputSchema["type"] != "object" {
			t.Errorf("tool %q schema = %#v", def.Name, def.InputSchema)
		}
	}
	for _, eager := range []string{
		"read", "write", "grep", "exec", "web", "sessions_spawn", "heartbeat_update",
		"goal", "mail_archive", "transcribe", "ocr", "org", "message",
	} {
		if !seen[eager] {
			t.Errorf("minimal core missing %q", eager)
		}
	}
	for _, optional := range []string{"research_panel", "workfeed", "market", "read_spillover"} {
		if seen[optional] {
			t.Errorf("minimal core unexpectedly registered optional %q", optional)
		}
	}
}

func TestRegisterCoreToolsDeferredPolicyContractMatchesOperationalIntent(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterCoreTools(reg, &tooldeps.CoreToolDeps{WorkspaceDir: t.TempDir()})
	deferred := map[string]bool{
		"read": false, "write": false, "grep": false, "exec": false, "web": false,
		"sessions_spawn": false, "heartbeat_update": false, "goal": false,
		"mail_archive": false, "transcribe": true, "ocr": true, "org": true,
		"office": false, // eager: document work is a core operator workflow
		"edit":   true, "gateway": true, "observe": true, "fleet": true, "browser": true, "groupware": false,
		"graphify": true, "process": true, "sessions": true, "subagents": true,
		"message": true, "todo": true, "cron": true, "files": true,
		"morning_letter": true, "evening_letter": true,
		"send_file": true, "chart": true, "diagram": true, "watch": true,
		"phone_read": true, "phone_write": true,
	}
	for name, want := range deferred {
		def := registeredTool(t, reg, name)
		if def.Deferred != want {
			t.Errorf("%s deferred = %v, want %v", name, def.Deferred, want)
		}
	}
}

func TestWorkspaceRegistrationGroupsPreserveOrder(t *testing.T) {
	noop := toolport.ToolFunc(func(context.Context, json.RawMessage) (string, error) { return "", nil })
	reg := &mockRegistrar{}
	RegisterFileTools(reg, t.TempDir())
	core.RegisterRuntimeOpsTools(reg, core.RuntimeOpsToolSet{
		Gateway:       noop,
		Observe:       noop,
		Fleet:         noop,
		Browser:       noop,
		Groupware:     noop,
		SpilloverRead: noop,
	})
	core.RegisterGraphTool(reg, t.TempDir())

	want := []string{"read", "write", "edit", "grep", "gateway", "observe", "fleet", "browser", "groupware", "read_spillover", "graphify"}
	if got := reg.toolNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registration order = %#v, want %#v", got, want)
	}
}

func TestRegistrationGroupsEnforceExactNamesWithoutCrossGroupDuplicates(t *testing.T) {
	type group struct {
		name string
		run  func(*mockRegistrar)
		want []string
	}
	noop := toolport.ToolFunc(func(context.Context, json.RawMessage) (string, error) { return "", nil })
	groups := []group{
		{name: "file", run: func(r *mockRegistrar) { RegisterFileTools(r, t.TempDir()) }, want: []string{"edit", "grep", "read", "write"}},
		{name: "runtime-ops", run: func(r *mockRegistrar) {
			core.RegisterRuntimeOpsTools(r, core.RuntimeOpsToolSet{Gateway: noop, Observe: noop, Fleet: noop, Browser: noop, Groupware: noop})
		}, want: []string{"browser", "fleet", "gateway", "groupware", "observe"}},
		{name: "graph", run: func(r *mockRegistrar) { core.RegisterGraphTool(r, t.TempDir()) }, want: []string{"graphify"}},
		{name: "phone", run: func(r *mockRegistrar) { core.RegisterPhoneTools(r, nil) }, want: []string{"phone_read", "phone_write"}},
		{name: "process", run: func(r *mockRegistrar) { RegisterProcessTools(r, &tooldeps.ProcessDeps{WorkspaceDir: t.TempDir()}) }, want: []string{"exec", "process"}},
		{name: "web", run: func(r *mockRegistrar) { core.RegisterWebTools(r, nil) }, want: []string{"web", "browse"}},
		{name: "session", run: func(r *mockRegistrar) { RegisterSessionTools(r, &tooldeps.SessionDeps{}) }, want: []string{"sessions", "sessions_spawn", "subagents"}},
		{name: "chrono", run: func(r *mockRegistrar) { RegisterChronoTools(r) }, want: []string{"message", "heartbeat_update"}},
		{name: "todo", run: func(r *mockRegistrar) { chrono.RegisterTodoTool(r) }, want: []string{"todo"}},
		{name: "routine", run: func(r *mockRegistrar) { chrono.RegisterRoutineTools(r, &tooldeps.ChronoDeps{}, "", "", nil) }, want: []string{"cron", "evening_letter", "files", "morning_letter"}},
		{name: "skills", run: func(r *mockRegistrar) { RegisterSkillsTools(r, nil, t.TempDir(), "", nil) }, want: []string{"skills"}},
		{name: "media", run: func(r *mockRegistrar) { RegisterMediaTools(r, t.TempDir()) }, want: []string{"chart", "diagram", "send_file", "watch"}},
	}
	all := make(map[string]string)
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			reg := &mockRegistrar{}
			g.run(reg)
			got := reg.toolNames()
			sort.Strings(got)
			want := append([]string(nil), g.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("names = %#v, want %#v", got, want)
			}
			for _, def := range reg.tools {
				if prior, exists := all[def.Name]; exists {
					t.Errorf("tool %q appears in %s and %s", def.Name, prior, g.name)
				}
				all[def.Name] = g.name
				assertRegisteredContract(t, def, def.Deferred)
			}
		})
	}
}

func TestOptionalRegistrationsSkipUnavailableDependencies(t *testing.T) {
	tests := []struct {
		name string
		run  func(*mockRegistrar)
	}{
		{name: "polaris nil", run: func(r *mockRegistrar) { recall.RegisterPolarisTools(r, nil, nil) }},
		{name: "knowledge nil", run: func(r *mockRegistrar) { recall.RegisterKnowledgeTool(r, nil) }},
		{name: "contacts empty", run: func(r *mockRegistrar) { RegisterContactsTool(r, &tooldeps.ContactsDeps{}) }},
		{name: "calendar empty", run: func(r *mockRegistrar) { RegisterCalendarTool(r, &tooldeps.CalendarDeps{}) }},
		{name: "wiki empty", run: func(r *mockRegistrar) { RegisterWikiTools(r, &tooldeps.WikiDeps{}, t.TempDir(), nil) }},
		{name: "notebook nil", run: func(r *mockRegistrar) { RegisterNotebookTool(r, nil) }},
		{name: "notebook empty", run: func(r *mockRegistrar) { RegisterNotebookTool(r, &tooldeps.NotebookDeps{}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := &mockRegistrar{}
			tc.run(reg)
			if len(reg.tools) != 0 {
				t.Fatalf("optional registration = %v, want empty", reg.toolNames())
			}
		})
	}
}

func TestCalendarRegistrationRequiresEitherReaderOrLocalStore(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterCalendarTool(reg, &tooldeps.CalendarDeps{
		Client: func() (tooldeps.CalendarReader, error) { return nil, nil },
	})
	if got := reg.toolNames(); !reflect.DeepEqual(got, []string{"calendar"}) {
		t.Fatalf("reader-backed calendar names = %#v", got)
	}
	def := registeredTool(t, reg, "calendar")
	if def.Deferred || def.Fn == nil || def.InputSchema["type"] != "object" {
		t.Fatalf("calendar contract = %#v", def)
	}
}

func TestRegistrarPreservesIndependentSchemaCopies(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterMediaTools(reg, t.TempDir())
	if len(reg.tools) < 2 {
		t.Fatal("media fixture too small")
	}
	reg.tools[0].InputSchema["type"] = "mutated"
	if reg.tools[1].InputSchema["type"] != "object" {
		t.Fatal("registered tools share schema root maps")
	}
	fresh := &mockRegistrar{}
	RegisterMediaTools(fresh, t.TempDir())
	if fresh.tools[0].InputSchema["type"] != "object" {
		t.Fatal("registration reused schema from prior call")
	}
}

func TestConcurrentSchemaConstructionIsIsolated(t *testing.T) {
	cases := allSchemaCases()
	const rounds = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, len(cases)*rounds)
	for _, tc := range cases {
		for round := 0; round < rounds; round++ {
			wg.Add(1)
			go func(tc schemaCase, round int) {
				defer wg.Done()
				<-start
				schema := tc.build()
				if schema["type"] != "object" {
					errs <- fmt.Sprintf("%s[%d] type=%v", tc.name, round, schema["type"])
					return
				}
				if properties, ok := schema["properties"].(map[string]any); ok {
					properties[fmt.Sprintf("mutated-%d", round)] = map[string]any{"type": "string"}
				}
			}(tc, round)
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, tc := range cases {
		if properties, ok := tc.build()["properties"].(map[string]any); ok {
			for key := range properties {
				if strings.HasPrefix(key, "mutated-") {
					t.Errorf("%s retained concurrent mutation %q", tc.name, key)
				}
			}
		}
	}
}

func TestToolDefinitionsCanBeInvokedWithCancelledContextWithoutRegistrationPanic(t *testing.T) {
	// Registration must not eagerly execute tool implementations. Build every
	// dependency-free group under an already-cancelled context and retain the
	// functions for later dispatch; cancellation belongs to invocation time.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context not cancelled")
	}
	reg := &mockRegistrar{}
	core.RegisterPhoneTools(reg, nil)
	RegisterProcessTools(reg, &tooldeps.ProcessDeps{WorkspaceDir: t.TempDir()})
	core.RegisterWebTools(reg, nil)
	RegisterSessionTools(reg, &tooldeps.SessionDeps{})
	RegisterChronoTools(reg)
	chrono.RegisterRoutineTools(reg, &tooldeps.ChronoDeps{}, "", "", nil)
	RegisterSkillsTools(reg, nil, t.TempDir(), "", nil)
	RegisterMediaTools(reg, t.TempDir())
	for _, def := range reg.tools {
		if def.Fn == nil {
			t.Errorf("cancelled-context registration left %q without Fn", def.Name)
		}
	}
}

// TestRegisterWikiAndPersonaToolsExposePrivacySurfaces locks the agent tools that
// #3679 accidentally vacated during a health fanout cut — preference (SOUL.md)
// and wiki_forget (hard delete + session cache flush).
func TestRegisterWikiAndPersonaToolsExposePrivacySurfaces(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	reg := &mockRegistrar{}
	RegisterWikiTools(reg, &tooldeps.WikiDeps{Store: store}, dir, func(string) {})
	RegisterPersonaTools(reg, dir)

	got := map[string]toolport.ToolDef{}
	for _, def := range reg.tools {
		got[def.Name] = def
	}
	for _, name := range []string{"wiki", "wiki_forget", "preference"} {
		def, ok := got[name]
		if !ok {
			t.Fatalf("missing required tool %q; registered=%v", name, reg.toolNames())
		}
		if def.Fn == nil {
			t.Fatalf("%q registered without Fn", name)
		}
		if name != "wiki" && !def.Deferred {
			t.Fatalf("%q should be deferred (destructive/occasional)", name)
		}
	}
}
