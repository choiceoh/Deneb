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
		{"mail_archive", "search", true},
		{"mail_archive", "list", true},
		{"mail_archive", "project_history", true},
		{"gmail", "search", false},     // Gmail OAuth/account surface — not exposed
		{"gmail", "send", false},       // outbound — never on the bridge
		{"gmail", "reply", false},      // outbound
		{"gmail", "label", false},      // Gmail-account mutation — not exposed
		{"gmail", "attachment", false}, // attachment download may write outside the bridge
		{"calendar", "list", true},
		{"calendar", "free_slots", true},
		{"calendar", "create", true}, // internal write (local store)
		{"calendar", "update", true},
		{"calendar", "delete", true},
		{"contacts", "lookup", true},
		{"contacts", "search", true},
		{"wiki", "search", true},
		{"wiki", "read", true},
		{"wiki", "write", true}, // internal write (git-versioned)
		{"wiki", "log", true},
		{"read", "", true},  // workspace-clamped fs read
		{"write", "", true}, // workspace-clamped fs write
		{"edit", "", true},  // workspace-clamped fs edit
		{"exec", "", false}, // not on the bridge
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
	if err := codeActionAllow("mail_archive", map[string]any{}); err == nil {
		t.Error("mail_archive with no action should be rejected")
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
	if code, _ := post("wrong", `{"tool":"mail_archive","args":{"action":"search"}}`); code != http.StatusForbidden {
		t.Fatalf("wrong token: want 403, got %d", code)
	}

	// Allowed call → forwarded, result returned.
	code, out := post("secret-token", `{"tool":"mail_archive","args":{"action":"search","query":"탑솔라"}}`)
	if code != http.StatusOK || out["ok"] != true || out["result"] != "RESULT_OK" {
		t.Fatalf("allowed call: code=%d out=%v", code, out)
	}

	// Disallowed action → ok:false, invoker NOT called for it.
	_, out = post("secret-token", `{"tool":"gmail","args":{"action":"search","query":"x"}}`)
	if out["ok"] != false {
		t.Fatalf("gmail should be rejected, got %v", out)
	}

	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "mail_archive:") || !strings.Contains(calls[0], "search") {
		t.Fatalf("invoker should have been called once for the search, got %v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "gmail") {
			t.Fatalf("gmail must never reach the invoker, got %v", calls)
		}
	}
}

func TestCodeActionPromotionCallsSkillLifecycleAfterSuccessfulRun(t *testing.T) {
	inv := &recordingInvoker{result: `{"ok":true,"route":"genesis","executed":true}`}
	out := promoteCodeActionWorkflow(context.Background(), inv, CodeActionPromotion{
		Candidate: "Batch join contacts calendar and wiki in code_action",
		Evidence:  "used structured as_json joins and local wiki write",
	}, true)
	if !strings.Contains(out, "code_action skill promotion") || !strings.Contains(out, `"executed":true`) {
		t.Fatalf("unexpected promotion output: %s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "skill_lifecycle:") {
		t.Fatalf("expected one skill_lifecycle call, got %v", calls)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(calls[0], "skill_lifecycle:")), &payload); err != nil {
		t.Fatalf("decode promotion payload: %v", err)
	}
	if payload["action"] != "propose" ||
		payload["candidate"] != "Batch join contacts calendar and wiki in code_action" ||
		payload["route"] != "genesis" ||
		payload["execute"] != true ||
		!strings.Contains(payload["evidence"].(string), "successful code_action workflow") {
		t.Fatalf("unexpected promotion payload: %+v", payload)
	}
}

func TestCodeActionPromotionSkipsFailedRun(t *testing.T) {
	inv := &recordingInvoker{result: "SHOULD_NOT_CALL"}
	out := promoteCodeActionWorkflow(context.Background(), inv, CodeActionPromotion{
		Candidate: "Failed workflow",
	}, false)
	if !strings.Contains(out, "skipped") {
		t.Fatalf("expected skipped promotion, got %s", out)
	}
	if calls := inv.called(); len(calls) != 0 {
		t.Fatalf("failed code_action must not call skill_lifecycle, got %v", calls)
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
// permitted deneb.mail_archive call reaches the (fake) invoker.
func TestCodeAction_OutputAndBridge(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "MAILS:a,b,c"}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `
mails = deneb.mail_archive("search", query="탑솔라", limit=5)
print("GOT", mails)
`)
	if !strings.Contains(out, "GOT MAILS:a,b,c") {
		t.Fatalf("expected bridge result echoed, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.Contains(calls[0], `"action":"search"`) || !strings.Contains(calls[0], "탑솔라") {
		t.Fatalf("expected one mail_archive search call, got %v", calls)
	}
}

func TestCodeAction_MailArchiveBridge(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "ARCHIVE:thread"}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `
thread = deneb.mail_archive("thread", message_id="archive|INBOX|1", limit=20)
print("GOT", thread)
`)
	if !strings.Contains(out, "GOT ARCHIVE:thread") {
		t.Fatalf("expected mail_archive bridge result echoed, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "mail_archive:") || !strings.Contains(calls[0], `"action":"thread"`) {
		t.Fatalf("expected one mail_archive thread call, got %v", calls)
	}
}

func TestCodeAction_MailArchiveStructuredJSON(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: `{"action":"list","messages":[{"subject":"Alpha","locator":"archive|INBOX|1"}]}`}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `
rows = deneb.mail_archive("list", as_json=True)
print(rows["messages"][0]["subject"], rows["messages"][0]["locator"])
`)
	if !strings.Contains(out, "Alpha archive|INBOX|1") {
		t.Fatalf("expected structured mail_archive JSON decoded, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "mail_archive:") || !strings.Contains(calls[0], `"as_json":true`) {
		t.Fatalf("expected one mail_archive JSON call, got %v", calls)
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

	// gmail is intentionally absent from the code_action bridge.
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

	// index → page paths in a category (enables enumerate-then-read batches).
	iv, err := wikiStructured(context.Background(), store2, map[string]any{"action": "index", "category": "프로젝트"})
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := iv.([]string)
	if !ok {
		t.Fatalf("index want []string, got %T", iv)
	}
	foundPath := false
	for _, p := range paths {
		if strings.Contains(p, "topsolar") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("index should list the page, got %v", paths)
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

// TestCodeAction_StructuredWikiIndex is the python e2e for deneb.wiki("index",
// as_json=True): the full subprocess→bridge→ListPages path yields a path list
// the model can enumerate and then read.
func TestCodeAction_StructuredWikiIndex(t *testing.T) {
	requirePython(t)
	dir := t.TempDir()
	wdir := filepath.Join(dir, "wiki")
	store, err := wiki.NewStore(wdir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("프로젝트/topsolar.md", &wiki.Page{
		Meta: wiki.Frontmatter{Title: "탑솔라"}, Body: "x",
	}); err != nil {
		t.Fatal(err)
	}
	store2, err := wiki.NewStore(wdir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	out := runCodeAction(t, CodeActionDeps{Invoker: &recordingInvoker{}, Wiki: store2}, `
paths = deneb.wiki("index", category="프로젝트", as_json=True)
print("TYPE", type(paths).__name__, "N", len(paths))
print("HAS", any("topsolar" in p for p in paths))
`)
	if !strings.Contains(out, "TYPE list") || !strings.Contains(out, "HAS True") {
		t.Fatalf("wiki index as_json should yield a path list, got:\n%s", out)
	}
}

// TestCodeAction_InternalWrites confirms internal writes (calendar create, wiki
// write, fs write) forward to the tool registry, while Gmail access is rejected
// inside the code_action runtime and never reaches the invoker.
func TestCodeAction_InternalWrites(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "ok"}
	out := runCodeAction(t, CodeActionDeps{Invoker: inv}, `
deneb.calendar("create", summary="탑솔라 미팅", start="2026-06-20T15:00:00+09:00")
deneb.wiki("write", query="프로젝트/x.md", title="X", content="body")
deneb.write("note.txt", "hello")
try:
    deneb.gmail("send", to="a@b.c", body="x")
    print("LEAK: send allowed")
except Exception as e:
    print("BLOCKED:", "not available" in str(e))
`)
	if strings.Contains(out, "LEAK") {
		t.Fatalf("gmail send must be blocked, got:\n%s", out)
	}
	if !strings.Contains(out, "BLOCKED: True") {
		t.Fatalf("send should raise a clear block, got:\n%s", out)
	}

	var hasCreate, hasWikiWrite, hasFsWrite, hasSend bool
	for _, c := range inv.called() {
		switch {
		case strings.HasPrefix(c, "calendar:") && strings.Contains(c, `"action":"create"`):
			hasCreate = true
		case strings.HasPrefix(c, "wiki:") && strings.Contains(c, `"action":"write"`):
			hasWikiWrite = true
		case strings.HasPrefix(c, "write:") && strings.Contains(c, "note.txt"):
			hasFsWrite = true
		}
		if strings.Contains(c, `"action":"send"`) {
			hasSend = true
		}
	}
	if !hasCreate || !hasWikiWrite || !hasFsWrite {
		t.Fatalf("internal writes should forward to the invoker; calls=%v", inv.called())
	}
	if hasSend {
		t.Fatalf("gmail send must never reach the invoker; calls=%v", inv.called())
	}
}
