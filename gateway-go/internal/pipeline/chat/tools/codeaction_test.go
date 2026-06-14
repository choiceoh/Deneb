package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// recordingInvoker captures the tool calls the bridge forwards and returns a
// canned result, so the security boundary can be tested without real tools.
type recordingInvoker struct {
	mu     sync.Mutex
	calls  []string // "name:argsJSON"
	result string
}

func (r *recordingInvoker) Execute(_ context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+":"+string(input))
	r.mu.Unlock()
	return r.result, nil
}

func (r *recordingInvoker) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// TestCodeActionAllow pins the read-only allowlist: read actions pass, every
// mutating / outbound action and unknown tool is rejected.
func TestCodeActionAllow(t *testing.T) {
	cases := []struct {
		tool   string
		action string
		ok     bool
	}{
		{"gmail", "search", true},
		{"gmail", "inbox", true},
		{"gmail", "analyze", true},
		{"gmail", "send", false},
		{"gmail", "reply", false},
		{"gmail", "label", false},
		{"gmail", "attachment", false},
		{"calendar", "list", true},
		{"calendar", "free_slots", true},
		{"calendar", "create", false},
		{"calendar", "delete", false},
		{"contacts", "lookup", true},
		{"contacts", "search", true},
		{"wiki", "search", true},
		{"wiki", "read", true},
		{"wiki", "write", false},
		{"wiki", "log", false},
		{"read", "", true}, // read self-clamps to workspace+skills roots
		{"exec", "", false},
		{"fs", "", false},
	}
	for _, c := range cases {
		args := map[string]any{}
		if c.action != "" {
			args["action"] = c.action
		}
		err := codeActionAllow(c.tool, args)
		if c.ok && err != nil {
			t.Errorf("%s/%s: expected allowed, got %v", c.tool, c.action, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s/%s: expected rejected, got nil", c.tool, c.action)
		}
	}

	// A known tool with a missing action is rejected (not silently allowed).
	if err := codeActionAllow("gmail", map[string]any{}); err == nil {
		t.Error("gmail with no action should be rejected")
	}
}

// TestCodeActionBridge covers the HTTP bridge: token auth, allowlist
// enforcement, and forwarding of permitted calls to the invoker.
func TestCodeActionBridge(t *testing.T) {
	inv := &recordingInvoker{result: "RESULT_OK"}
	b := &codeActionBridge{invoker: inv, token: "secret-token", ctx: context.Background()}
	srv := httptest.NewServer(b)
	defer srv.Close()

	post := func(token, body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
		req.Header.Set("X-Deneb-Bridge-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// Wrong token → 403, invoker untouched.
	if code, _ := post("wrong", `{"tool":"gmail","args":{"action":"search"}}`); code != http.StatusForbidden {
		t.Fatalf("wrong token: want 403, got %d", code)
	}

	// Allowed call → forwarded, result returned.
	code, out := post("secret-token", `{"tool":"gmail","args":{"action":"search","query":"탑솔라"}}`)
	if code != http.StatusOK || out["ok"] != true || out["result"] != "RESULT_OK" {
		t.Fatalf("allowed call: code=%d out=%v", code, out)
	}

	// Disallowed action → ok:false, invoker NOT called for it.
	_, out = post("secret-token", `{"tool":"gmail","args":{"action":"send","to":"x","body":"y"}}`)
	if out["ok"] != false {
		t.Fatalf("send should be rejected, got %v", out)
	}

	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "gmail:") || !strings.Contains(calls[0], "search") {
		t.Fatalf("invoker should have been called once for the search, got %v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "send") {
			t.Fatalf("send must never reach the invoker, got %v", calls)
		}
	}
}

// requirePython skips the test if python3 is not on PATH (CI has no GPU host
// toolchain guarantee). The pure-Go tests above always run.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping code_action sandbox e2e")
	}
}

func runCodeAction(t *testing.T, d CodeActionDeps, code string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"code": code, "timeout": 30})
	out, err := ToolCodeAction(d)(context.Background(), json.RawMessage(in))
	if err != nil {
		t.Fatalf("ToolCodeAction: %v", err)
	}
	return out
}

// newTestContactsStore builds a populated address book for the structured path.
func newTestContactsStore(t *testing.T) *contacts.Store {
	t.Helper()
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAll([]contacts.Contact{
		{Name: "오선택", Phones: []string{"010-9945-4849"}, Emails: []string{"choiceoh@example.com"}, Org: "탑솔라"},
		{Name: "김민준", Phones: []string{"010-1111-2222"}, Org: "탑솔라에코"},
		{Name: "이서연", Phones: []string{"010-3333-4444"}, Org: "다른회사"},
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

// TestCodeAction_OutputAndBridge runs real Python: print() round-trips and a
// permitted deneb.gmail call reaches the (fake) invoker.
func TestCodeAction_OutputAndBridge(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "MAILS:a,b,c"}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `
mails = deneb.gmail("search", query="탑솔라", max=5)
print("GOT", mails)
`)
	if !strings.Contains(out, "GOT MAILS:a,b,c") {
		t.Fatalf("expected bridge result echoed, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.Contains(calls[0], `"action":"search"`) || !strings.Contains(calls[0], "탑솔라") {
		t.Fatalf("expected one gmail search call, got %v", calls)
	}
}

// TestCodeAction_SandboxBlocks is the security core: the audit hook must block
// network, subprocess, out-of-sandbox writes, and secret reads — each surfacing
// as a traceback the model can read.
func TestCodeAction_SandboxBlocks(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: ""}

	cases := []struct {
		name string
		code string
		want string
	}{
		{
			"network",
			`import socket; socket.create_connection(("1.1.1.1", 80), timeout=2)`,
			"network is disabled",
		},
		{
			"subprocess",
			`import os; os.system("echo pwned")`,
			"spawning processes is disabled",
		},
		{
			"write_outside",
			`open("/tmp/deneb-codeaction-escape.txt", "w").write("x")`,
			"writes are limited to the scratch directory",
		},
		{
			"secret_read",
			`import os; open(os.path.expanduser("~/.deneb/deneb.json"))`,
			"is disabled",
		},
		{
			"import_subprocess",
			`import subprocess`,
			"is disabled",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runCodeAction(t, CodeActionDeps{Invoker: inv}, c.code)
			if !strings.Contains(out, c.want) {
				t.Fatalf("expected block %q, got:\n%s", c.want, out)
			}
		})
	}
}

// TestCodeAction_WriteInSandboxAllowed confirms the confinement is not
// over-broad: writing inside the scratch dir works.
func TestCodeAction_WriteInSandboxAllowed(t *testing.T) {
	requirePython(t)
	out := runCodeAction(t, CodeActionDeps{Invoker: &recordingInvoker{}}, `
import os
p = os.path.join(os.environ["DENEB_SANDBOX_DIR"], "scratch.txt")
open(p, "w").write("hello")
print("WROTE", open(p).read())
`)
	if !strings.Contains(out, "WROTE hello") {
		t.Fatalf("sandbox-local write should succeed, got:\n%s", out)
	}
	if strings.Contains(out, "sandbox:") {
		t.Fatalf("unexpected sandbox block on a legal write:\n%s", out)
	}
}

// TestContactsStructured covers the typed structured-output handler: read
// actions return []Contact, empty results are non-nil, mutating actions reject.
func TestContactsStructured(t *testing.T) {
	store := newTestContactsStore(t)

	val, err := contactsStructured(store, map[string]any{"action": "search", "query": "탑솔라"})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := val.([]contacts.Contact)
	if !ok {
		t.Fatalf("want []contacts.Contact, got %T", val)
	}
	found := false
	for _, c := range list {
		if c.Name == "오선택" {
			found = true
		}
	}
	if !found {
		t.Fatalf("탑솔라 search should include 오선택, got %+v", list)
	}

	// Empty result is a non-nil slice (Python decodes to [], not None).
	empty, err := contactsStructured(store, map[string]any{"action": "search", "query": "존재하지않는검색어zzz"})
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil {
		t.Fatal("empty result must be a non-nil slice")
	}

	// A non-read action is rejected even on the structured path.
	if _, err := contactsStructured(store, map[string]any{"action": "create"}); err == nil {
		t.Fatal("contacts structured 'create' must be rejected")
	}
}

// TestCodeActionBridge_structured verifies the json=true path: contacts returns
// a marshaled []Contact, and a tool without a structured handler errors clearly.
func TestCodeActionBridge_structured(t *testing.T) {
	store := newTestContactsStore(t)
	b := &codeActionBridge{invoker: &recordingInvoker{}, contacts: store, token: "tok", ctx: context.Background()}
	srv := httptest.NewServer(b)
	defer srv.Close()

	post := func(body string) map[string]any {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
		req.Header.Set("X-Deneb-Bridge-Token", "tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// contacts structured → result is a JSON array string including 오선택.
	out := post(`{"tool":"contacts","args":{"action":"search","query":"탑솔라"},"json":true}`)
	if out["ok"] != true {
		t.Fatalf("structured contacts should succeed, got %v", out)
	}
	result, _ := out["result"].(string)
	if !strings.HasPrefix(strings.TrimSpace(result), "[") || !strings.Contains(result, "오선택") {
		t.Fatalf("structured result should be a JSON array with 오선택, got %q", result)
	}

	// gmail has no structured handler → clear error, not a crash.
	out = post(`{"tool":"gmail","args":{"action":"search","query":"x"},"json":true}`)
	if out["ok"] != false {
		t.Fatalf("structured gmail should be rejected, got %v", out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "not available") {
		t.Fatalf("expected 'not available' error, got %q", msg)
	}
}

// TestCodeAction_StructuredContacts is the python e2e for as_json=True: model
// code receives a real Python list of dicts it can filter/count.
func TestCodeAction_StructuredContacts(t *testing.T) {
	requirePython(t)
	store := newTestContactsStore(t)
	out := runCodeAction(t, CodeActionDeps{Invoker: &recordingInvoker{}, Contacts: store}, `
rows = deneb.contacts("search", "탑솔라", as_json=True)
print("TYPE", type(rows).__name__)
hits = [r for r in rows if "탑솔라" in (r.get("org") or "")]
print("COUNT", len(hits))
print("FIRST", hits[0]["name"] if hits else "none")
`)
	if !strings.Contains(out, "TYPE list") {
		t.Fatalf("as_json=True should yield a Python list, got:\n%s", out)
	}
	if strings.Contains(out, "COUNT 0") || !strings.Contains(out, "COUNT") {
		t.Fatalf("expected at least one 탑솔라 contact, got:\n%s", out)
	}
}

// TestCodeAction_ReadThroughBridge confirms deneb.read reaches the read tool
// (the read tool's own workspace clamping is covered by fs tests).
func TestCodeAction_ReadThroughBridge(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "FILE_CONTENTS_HERE"}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `print(deneb.read("notes.txt"))`)
	if !strings.Contains(out, "FILE_CONTENTS_HERE") {
		t.Fatalf("deneb.read should return the read tool result, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "read:") || !strings.Contains(calls[0], "notes.txt") {
		t.Fatalf("expected one read call, got %v", calls)
	}
}

// fakeLocalCal is a minimal LocalCalendar for the structured-calendar tests.
type fakeLocalCal struct{ events []calendar.Event }

func (f *fakeLocalCal) ListRange(_, _ time.Time) []calendar.Event { return f.events }
func (f *fakeLocalCal) Get(id string) *calendar.Event {
	for i := range f.events {
		if f.events[i].ID == id {
			return &f.events[i]
		}
	}
	return nil
}
func (f *fakeLocalCal) Create(localcal.CreateInput) (calendar.Event, error) {
	return calendar.Event{}, nil
}
func (f *fakeLocalCal) Update(string, localcal.CreateInput) (*calendar.Event, error) {
	return nil, nil
}
func (f *fakeLocalCal) Delete(string) error { return nil }

// TestCalendarStructured covers the typed calendar path: list maps merged
// events to DTOs, get routes a local: ID to the local store, mutating actions
// are rejected.
func TestCalendarStructured(t *testing.T) {
	start := time.Now().Add(time.Hour)
	fake := &fakeLocalCal{events: []calendar.Event{{
		ID: "local:evt1", Summary: "탑솔라 미팅", Start: start, End: start.Add(time.Hour),
		Location: "본사", Attendees: []calendar.Attendee{{Email: "a@x.com"}, {Email: "b@x.com"}},
	}}}
	d := &toolctx.CalendarDeps{Local: fake}

	val, err := calendarStructured(context.Background(), d, map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	evs, ok := val.([]caEvent)
	if !ok || len(evs) != 1 {
		t.Fatalf("want 1 event, got %T %v", val, val)
	}
	if evs[0].Title != "탑솔라 미팅" || evs[0].ID != "local:evt1" || len(evs[0].Attendees) != 2 {
		t.Fatalf("event mapped wrong: %+v", evs[0])
	}
	if evs[0].Start == "" || evs[0].End == "" {
		t.Fatalf("start/end should be RFC3339, got %+v", evs[0])
	}

	gv, err := calendarStructured(context.Background(), d, map[string]any{"action": "get", "id": "local:evt1"})
	if err != nil {
		t.Fatal(err)
	}
	if gv.(caEvent).Title != "탑솔라 미팅" {
		t.Fatalf("get returned wrong event: %+v", gv)
	}

	if _, err := calendarStructured(context.Background(), d, map[string]any{"action": "create"}); err == nil {
		t.Fatal("calendar structured 'create' must be rejected")
	}
}

// TestWikiStructured covers the typed wiki path: read returns page metadata +
// body, search returns ranked hits, mutating actions are rejected.
func TestWikiStructured(t *testing.T) {
	dir := t.TempDir()
	wdir := filepath.Join(dir, "wiki")
	store, err := wiki.NewStore(wdir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("프로젝트/topsolar.md", &wiki.Page{
		Meta: wiki.Frontmatter{Title: "탑솔라", Summary: "태양광 EPC", Category: "프로젝트", Tags: []string{"태양광"}},
		Body: "탑솔라는 태양광 EPC 회사. zorptest marker.",
	}); err != nil {
		t.Fatal(err)
	}
	// Fresh store rebuilds the in-memory FTS index from disk (a running index is
	// not guaranteed to pick up a just-written page).
	store2, err := wiki.NewStore(wdir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}

	rv, err := wikiStructured(context.Background(), store2, map[string]any{"action": "read", "query": "프로젝트/topsolar.md"})
	if err != nil {
		t.Fatal(err)
	}
	pg, ok := rv.(caWikiPage)
	if !ok || pg.Title != "탑솔라" || !strings.Contains(pg.Body, "zorptest") {
		t.Fatalf("read wrong: %T %+v", rv, rv)
	}

	sv, err := wikiStructured(context.Background(), store2, map[string]any{"action": "search", "query": "zorptest"})
	if err != nil {
		t.Fatal(err)
	}
	hits, ok := sv.([]caWikiHit)
	if !ok {
		t.Fatalf("want []caWikiHit, got %T", sv)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Path, "topsolar") {
		t.Fatalf("search should find the page, got %+v", hits)
	}

	if _, err := wikiStructured(context.Background(), store2, map[string]any{"action": "write"}); err == nil {
		t.Fatal("wiki structured 'write' must be rejected")
	}
}

// TestCodeAction_StructuredCalendar is the python e2e for deneb.calendar(as_json=True).
func TestCodeAction_StructuredCalendar(t *testing.T) {
	requirePython(t)
	start := time.Now().Add(time.Hour)
	fake := &fakeLocalCal{events: []calendar.Event{{
		ID: "local:evt1", Summary: "탑솔라 미팅", Start: start, End: start.Add(time.Hour),
	}}}
	out := runCodeAction(t, CodeActionDeps{Invoker: &recordingInvoker{}, Calendar: &toolctx.CalendarDeps{Local: fake}}, `
evs = deneb.calendar("list", as_json=True)
print("TYPE", type(evs).__name__, "N", len(evs))
print("TITLE", evs[0]["title"] if evs else "none")
`)
	if !strings.Contains(out, "TYPE list") || !strings.Contains(out, "TITLE 탑솔라 미팅") {
		t.Fatalf("calendar as_json should yield event dicts, got:\n%s", out)
	}
}
