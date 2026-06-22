package toolctx

import (
	"strings"
	"sync"
)

// notebook_binding.go holds the per-session "active notebook" binding for the
// NotebookLM-style grounding mode. When a session is bound to a notebook, the
// chat pipeline grounds every turn in that notebook's pinned sources (injected
// as a wire-only tail addition on the last user message) AND suppresses the
// broad memory recall — the notebook is the explicit, narrow scope, so running
// the whole-corpus recall alongside it would dilute "이 자료 위주로" and compete
// for the turn's input budget.
//
// Why this lives in toolctx (a leaf package): the notebook tool (the writer —
// open/close actions) and the chat run pipeline (the readers — run_exec.go,
// run_prepare.go, slash_dispatch.go) both need it. tools/ may not import chat/,
// so a chat-package store would be unreachable from the tool. toolctx is the
// shared leaf both already depend on, and it already holds comparable per-run
// state (RunCache, TurnContext).
//
// Mirrors chat/tier1_cache.go: a package-level map under an RWMutex, keyed by
// sessionKey, cleared by /reset. Process-global and single-user, so a plain map
// with coarse locking is enough. The binding is in-memory only (Phase 1): a
// gateway restart or a session reopen needs the notebook re-opened.

var activeNotebookStore = struct {
	mu    sync.RWMutex
	store map[string]string // sessionKey → active notebookID
}{store: make(map[string]string)}

// NotebookSessionPrefix marks a chat session as dedicated to one notebook: a
// session keyed "notebook:<id>" IS that notebook's conversation (the native
// "이 노트북으로 대화" entry opens such a session). Its active notebook is derived
// from the key itself — no explicit binding, deterministic, and it survives a
// gateway restart. Mirrors the "chat:" workspace-prefix convention. Because the
// key does not start with "chat:", such a session keeps the full chief-of-staff
// assistant (not the 챗봇 clean prompt), which is what a grounded notebook chat
// wants.
const NotebookSessionPrefix = "notebook:"

// DedicatedNotebookID returns the notebook id a "notebook:<id>" session is
// dedicated to, or "" for any other session key (including a bare "notebook:").
func DedicatedNotebookID(sessionKey string) string {
	if id := strings.TrimPrefix(sessionKey, NotebookSessionPrefix); id != sessionKey && id != "" {
		return id
	}
	return ""
}

// ActiveNotebook returns the notebook id grounding sessionKey, or "". A
// dedicated "notebook:<id>" session derives its notebook from the key; any
// other session uses the explicit binding set by the notebook tool's open
// action. Nil-safe on an empty key.
func ActiveNotebook(sessionKey string) string {
	if sessionKey == "" {
		return ""
	}
	if id := DedicatedNotebookID(sessionKey); id != "" {
		return id
	}
	activeNotebookStore.mu.RLock()
	defer activeNotebookStore.mu.RUnlock()
	return activeNotebookStore.store[sessionKey]
}

// SetActiveNotebook binds sessionKey to notebookID. Last-write-wins (unlike the
// first-write-wins snapshot caches) so opening a different notebook in the same
// session switches the active scope. Empty key or id is a no-op.
func SetActiveNotebook(sessionKey, notebookID string) {
	if sessionKey == "" || notebookID == "" {
		return
	}
	activeNotebookStore.mu.Lock()
	defer activeNotebookStore.mu.Unlock()
	activeNotebookStore.store[sessionKey] = notebookID
}

// ClearActiveNotebook unbinds sessionKey, returning it to ordinary (recall-on)
// chat. Called by the notebook tool's close action and the /reset handler; safe
// for sessions that were never bound.
func ClearActiveNotebook(sessionKey string) {
	if sessionKey == "" {
		return
	}
	activeNotebookStore.mu.Lock()
	defer activeNotebookStore.mu.Unlock()
	delete(activeNotebookStore.store, sessionKey)
}
