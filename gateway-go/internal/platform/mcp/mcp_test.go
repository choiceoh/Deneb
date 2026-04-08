package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// types.go
// ---------------------------------------------------------------------------

func TestIsNotification_NilID(t *testing.T) {
	t.Parallel()
	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "ping"}
	if !req.IsNotification() {
		t.Error("expected nil ID to be notification")
	}
}

func TestIsNotification_NullID(t *testing.T) {
	t.Parallel()
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: json.RawMessage("null"), Method: "ping"}
	if !req.IsNotification() {
		t.Error("expected \"null\" ID to be notification")
	}
}

func TestIsNotification_NumericID(t *testing.T) {
	t.Parallel()
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "ping"}
	if req.IsNotification() {
		t.Error("expected numeric ID to NOT be notification")
	}
}

func TestIsNotification_StringID(t *testing.T) {
	t.Parallel()
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`"abc"`), Method: "ping"}
	if req.IsNotification() {
		t.Error("expected string ID to NOT be notification")
	}
}

func TestTextContent(t *testing.T) {
	t.Parallel()
	got := TextContent("hello world")
	want := ContentBlock{Type: "text", Text: "hello world"}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("TextContent mismatch:\n  want: %+v\n   got: %+v", want, got)
	}
}

func TestMakeResponse(t *testing.T) {
	t.Parallel()
	id := json.RawMessage("42")
	resp, err := MakeResponse(id, map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("MakeResponse error: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if string(resp.ID) != "42" {
		t.Errorf("expected ID 42, got %s", string(resp.ID))
	}
	if resp.Error != nil {
		t.Error("expected nil error")
	}

	// Verify result payload round-trips correctly.
	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected result key=value, got %s", result["key"])
	}
}

func TestMakeResponse_MarshalError(t *testing.T) {
	t.Parallel()
	// Channels cannot be marshaled to JSON.
	_, err := MakeResponse(json.RawMessage("1"), make(chan int))
	if err == nil {
		t.Error("expected marshal error for unmarshalable type")
	}
}

func TestMakeErrorResponse(t *testing.T) {
	t.Parallel()
	id := json.RawMessage(`"req-1"`)
	resp := MakeErrorResponse(id, ErrCodeMethodNotFound, "method not found")
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if string(resp.ID) != `"req-1"` {
		t.Errorf("expected ID \"req-1\", got %s", string(resp.ID))
	}
	if resp.Result != nil {
		t.Error("expected nil result on error response")
	}
	if resp.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
	if resp.Error.Message != "method not found" {
		t.Errorf("expected message %q, got %q", "method not found", resp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// transport.go
// ---------------------------------------------------------------------------

// testLogger returns a quiet logger suitable for tests (only errors printed).
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestTransport_ReadRequest(t *testing.T) {
	t.Parallel()
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n"
	r := bytes.NewBufferString(input)
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	req, err := tr.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest error: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", req.JSONRPC)
	}
	if req.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", req.Method)
	}
	if string(req.ID) != "1" {
		t.Errorf("expected ID 1, got %s", string(req.ID))
	}
}

func TestTransport_ReadRequest_InvalidJSON(t *testing.T) {
	t.Parallel()
	r := bytes.NewBufferString("not json\n")
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	_, err := tr.ReadRequest()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTransport_ReadRequest_EOF(t *testing.T) {
	t.Parallel()
	r := bytes.NewBufferString("") // empty reader
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	_, err := tr.ReadRequest()
	if err == nil {
		t.Error("expected EOF error for empty reader")
	}
}

func TestTransport_WriteResponse(t *testing.T) {
	t.Parallel()
	r := bytes.NewBufferString("")
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Result:  json.RawMessage(`{"ok":true}`),
	}
	if err := tr.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse error: %v", err)
	}

	output := w.String()
	// Must end with newline.
	if output[len(output)-1] != '\n' {
		t.Error("expected output to end with newline")
	}
	// Strip trailing newline and parse.
	var got JSONRPCResponse
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("unmarshal written response: %v", err)
	}
	if got.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", got.JSONRPC)
	}
	if string(got.ID) != "1" {
		t.Errorf("expected ID 1, got %s", string(got.ID))
	}
}

func TestTransport_WriteNotification(t *testing.T) {
	t.Parallel()
	r := bytes.NewBufferString("")
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	notif := &Notification{
		JSONRPC: "2.0",
		Method:  "notifications/resources/updated",
		Params:  json.RawMessage(`{"uri":"deneb://status"}`),
	}
	if err := tr.WriteNotification(notif); err != nil {
		t.Fatalf("WriteNotification error: %v", err)
	}

	output := w.String()
	if output[len(output)-1] != '\n' {
		t.Error("expected output to end with newline")
	}
	var got Notification
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("unmarshal written notification: %v", err)
	}
	if got.Method != "notifications/resources/updated" {
		t.Errorf("expected method notifications/resources/updated, got %s", got.Method)
	}
}

func TestTransport_SendRequest(t *testing.T) {
	t.Parallel()
	r := bytes.NewBufferString("")
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage("99"),
		Method:  "sampling/createMessage",
		Params:  json.RawMessage(`{"maxTokens":1024}`),
	}
	if err := tr.SendRequest(req); err != nil {
		t.Fatalf("SendRequest error: %v", err)
	}

	output := w.String()
	if output[len(output)-1] != '\n' {
		t.Error("expected output to end with newline")
	}
	var got JSONRPCRequest
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("unmarshal written request: %v", err)
	}
	if got.Method != "sampling/createMessage" {
		t.Errorf("expected method sampling/createMessage, got %s", got.Method)
	}
	if string(got.ID) != "99" {
		t.Errorf("expected ID 99, got %s", string(got.ID))
	}
}

func TestTransport_ReadMultipleRequests(t *testing.T) {
	t.Parallel()
	input := `{"jsonrpc":"2.0","id":1,"method":"a"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"b"}` + "\n"
	r := bytes.NewBufferString(input)
	w := &bytes.Buffer{}
	tr := NewTransport(r, w, testLogger())

	req1, err := tr.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest 1 error: %v", err)
	}
	if req1.Method != "a" {
		t.Errorf("expected method a, got %s", req1.Method)
	}

	req2, err := tr.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest 2 error: %v", err)
	}
	if req2.Method != "b" {
		t.Errorf("expected method b, got %s", req2.Method)
	}
}

// ---------------------------------------------------------------------------
// tools.go
// ---------------------------------------------------------------------------

func TestNewToolRegistry_NotEmpty(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	tools := reg.List()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool registry")
	}
}

func TestToolRegistry_List_CountMatchesAllTools(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	want := len(allTools())
	got := len(reg.List())
	if got != want {
		t.Errorf("expected %d tools, got %d", want, got)
	}
}

func TestToolRegistry_List_ToolsHaveRequiredFields(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	for _, tool := range reg.List() {
		if tool.Name == "" {
			t.Error("tool has empty Name")
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil InputSchema", tool.Name)
		}
		schemaType, ok := tool.InputSchema["type"]
		if !ok || schemaType != "object" {
			t.Errorf("tool %s InputSchema type is not object", tool.Name)
		}
	}
}

func TestToolRegistry_UniqueNames(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	seen := make(map[string]bool)
	for _, tool := range reg.List() {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name: %s", tool.Name)
		}
		seen[tool.Name] = true
	}
}

func TestToolRegistry_Lookup_Known(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	rpcMethod, ok := reg.Lookup("deneb_chat_send")
	if !ok {
		t.Fatal("expected deneb_chat_send to be found")
	}
	if rpcMethod != "chat.send" {
		t.Errorf("expected rpc method chat.send, got %s", rpcMethod)
	}
}

func TestToolRegistry_Lookup_AllToolsResolvable(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	for _, tool := range reg.List() {
		rpcMethod, ok := reg.Lookup(tool.Name)
		if !ok {
			t.Errorf("tool %s not found in lookup", tool.Name)
		}
		if rpcMethod == "" {
			t.Errorf("tool %s has empty rpc method", tool.Name)
		}
	}
}

func TestToolRegistry_Lookup_Unknown(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	_, ok := reg.Lookup("nonexistent_tool")
	if ok {
		t.Error("expected unknown tool to not be found")
	}
}

func TestObjectSchema_NoProps(t *testing.T) {
	t.Parallel()
	schema := objectSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type object, got %v", schema["type"])
	}
	if _, exists := schema["properties"]; exists {
		t.Error("expected no properties key for empty schema")
	}
	if _, exists := schema["required"]; exists {
		t.Error("expected no required key for empty schema")
	}
}

func TestObjectSchema_RequiredAndOptionalProps(t *testing.T) {
	t.Parallel()
	schema := objectSchema(
		requiredProp("name", "string", "The name"),
		prop("age", "integer", "The age"),
		requiredProp("email", "string", "The email"),
	)

	if schema["type"] != "object" {
		t.Errorf("expected type object, got %v", schema["type"])
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be map[string]any")
	}
	if len(properties) != 3 {
		t.Errorf("expected 3 properties, got %d", len(properties))
	}

	// Check each property structure.
	for _, name := range []string{"name", "age", "email"} {
		p, exists := properties[name]
		if !exists {
			t.Errorf("expected property %s", name)
			continue
		}
		pm, ok := p.(map[string]any)
		if !ok {
			t.Errorf("property %s is not map[string]any", name)
			continue
		}
		if pm["type"] == nil {
			t.Errorf("property %s missing type", name)
		}
		if pm["description"] == nil {
			t.Errorf("property %s missing description", name)
		}
	}

	// Verify required list.
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required to be []string")
	}
	sort.Strings(required)
	wantRequired := []string{"email", "name"}
	if !reflect.DeepEqual(wantRequired, required) {
		t.Errorf("required mismatch:\n  want: %v\n   got: %v", wantRequired, required)
	}
}

func TestObjectSchema_AllOptional(t *testing.T) {
	t.Parallel()
	schema := objectSchema(
		prop("a", "string", "desc a"),
		prop("b", "integer", "desc b"),
	)

	if _, exists := schema["required"]; exists {
		t.Error("expected no required key when all props are optional")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be map[string]any")
	}
	if len(properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(properties))
	}
}

func TestProp_Structure(t *testing.T) {
	t.Parallel()
	p := prop("foo", "string", "a foo field")
	if p.name != "foo" {
		t.Errorf("expected name foo, got %s", p.name)
	}
	if p.typ != "string" {
		t.Errorf("expected type string, got %s", p.typ)
	}
	if p.description != "a foo field" {
		t.Errorf("expected description, got %s", p.description)
	}
	if p.required {
		t.Error("expected prop to not be required")
	}
}

func TestRequiredProp_Structure(t *testing.T) {
	t.Parallel()
	p := requiredProp("bar", "integer", "a bar field")
	if p.name != "bar" {
		t.Errorf("expected name bar, got %s", p.name)
	}
	if p.typ != "integer" {
		t.Errorf("expected type integer, got %s", p.typ)
	}
	if !p.required {
		t.Error("expected requiredProp to be required")
	}
}

// ---------------------------------------------------------------------------
// resources.go
// ---------------------------------------------------------------------------

func TestResourceManager_List(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	resources := rm.List()
	if len(resources) == 0 {
		t.Fatal("expected non-empty resource list")
	}

	// Verify each resource has required fields.
	for _, r := range resources {
		if r.URI == "" {
			t.Error("resource has empty URI")
		}
		if r.Name == "" {
			t.Error("resource has empty Name")
		}
	}
}

func TestResourceManager_List_ContainsExpectedURIs(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	resources := rm.List()

	expectedURIs := []string{
		"deneb://status",
		"deneb://sessions",
		"deneb://config",
		"deneb://skills",
		"deneb://models",
	}

	uriSet := make(map[string]bool, len(resources))
	for _, r := range resources {
		uriSet[r.URI] = true
	}
	for _, uri := range expectedURIs {
		if !uriSet[uri] {
			t.Errorf("expected resource URI %s not found", uri)
		}
	}
}

func TestResourceManager_SubscribeUnsubscribe(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)

	uri := "deneb://status"
	if rm.IsSubscribed(uri) {
		t.Error("expected URI to not be subscribed initially")
	}

	rm.Subscribe(uri)
	if !rm.IsSubscribed(uri) {
		t.Error("expected URI to be subscribed after Subscribe")
	}

	rm.Unsubscribe(uri)
	if rm.IsSubscribed(uri) {
		t.Error("expected URI to not be subscribed after Unsubscribe")
	}
}

func TestResourceManager_SubscribeMultiple(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)

	rm.Subscribe("deneb://status")
	rm.Subscribe("deneb://sessions")
	rm.Subscribe("deneb://config")

	uris := rm.SubscribedURIs()
	if len(uris) != 3 {
		t.Fatalf("expected 3 subscribed URIs, got %d", len(uris))
	}
	sort.Strings(uris)
	want := []string{"deneb://config", "deneb://sessions", "deneb://status"}
	if !reflect.DeepEqual(want, uris) {
		t.Errorf("subscribed URIs mismatch:\n  want: %v\n   got: %v", want, uris)
	}
}

func TestResourceManager_SubscribedURIs_Empty(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	uris := rm.SubscribedURIs()
	if len(uris) != 0 {
		t.Errorf("expected 0 subscribed URIs, got %d", len(uris))
	}
}

func TestResourceManager_SubscribeIdempotent(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rm.Subscribe("deneb://status")
	rm.Subscribe("deneb://status") // duplicate
	uris := rm.SubscribedURIs()
	if len(uris) != 1 {
		t.Errorf("expected 1 URI after duplicate subscribe, got %d", len(uris))
	}
}

func TestResourceManager_UnsubscribeNonexistent(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	// Should not panic on unsubscribing a URI that was never subscribed.
	rm.Unsubscribe("deneb://nonexistent")
	if rm.IsSubscribed("deneb://nonexistent") {
		t.Error("expected nonexistent URI to not be subscribed")
	}
}

func TestResolveURI_StaticStatus(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, params, err := rm.resolveURI("deneb://status")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "gateway.identity.get" {
		t.Errorf("expected gateway.identity.get, got %s", rpcMethod)
	}
	if params != nil {
		t.Errorf("expected nil params for static URI, got %v", params)
	}
}

func TestResolveURI_StaticSessions(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, params, err := rm.resolveURI("deneb://sessions")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "sessions.list" {
		t.Errorf("expected sessions.list, got %s", rpcMethod)
	}
	if params != nil {
		t.Errorf("expected nil params, got %v", params)
	}
}

func TestResolveURI_StaticConfig(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, _, err := rm.resolveURI("deneb://config")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "config.get" {
		t.Errorf("expected config.get, got %s", rpcMethod)
	}
}

func TestResolveURI_StaticSkills(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, _, err := rm.resolveURI("deneb://skills")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "skills.status" {
		t.Errorf("expected skills.status, got %s", rpcMethod)
	}
}

func TestResolveURI_StaticModels(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, _, err := rm.resolveURI("deneb://models")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "models.list" {
		t.Errorf("expected models.list, got %s", rpcMethod)
	}
}

func TestResolveURI_DynamicSessionKey(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, params, err := rm.resolveURI("deneb://sessions/my-session")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "sessions.preview" {
		t.Errorf("expected sessions.preview, got %s", rpcMethod)
	}
	keys, ok := params["keys"].([]string)
	if !ok {
		t.Fatalf("expected keys to be []string, got %T", params["keys"])
	}
	if len(keys) != 1 || keys[0] != "my-session" {
		t.Errorf("expected keys=[my-session], got %v", keys)
	}
}

func TestResolveURI_DynamicSessionHistory(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	rpcMethod, params, err := rm.resolveURI("deneb://sessions/my-session/history")
	if err != nil {
		t.Fatalf("resolveURI error: %v", err)
	}
	if rpcMethod != "sessions.preview" {
		t.Errorf("expected sessions.preview, got %s", rpcMethod)
	}
	keys, ok := params["keys"].([]string)
	if !ok {
		t.Fatalf("expected keys to be []string, got %T", params["keys"])
	}
	if len(keys) != 1 || keys[0] != "my-session" {
		t.Errorf("expected keys=[my-session] (history suffix stripped), got %v", keys)
	}
}

func TestResolveURI_UnknownURI(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	_, _, err := rm.resolveURI("deneb://nonexistent")
	if err == nil {
		t.Error("expected error for unknown URI")
	}
}

func TestResolveURI_EmptyURI(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	_, _, err := rm.resolveURI("")
	if err == nil {
		t.Error("expected error for empty URI")
	}
}

func TestResolveURI_CompletelyForeignScheme(t *testing.T) {
	t.Parallel()
	rm := NewResourceManager(nil)
	_, _, err := rm.resolveURI("https://example.com")
	if err == nil {
		t.Error("expected error for non-deneb URI")
	}
}
