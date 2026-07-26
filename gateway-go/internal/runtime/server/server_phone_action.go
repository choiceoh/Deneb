package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// phoneEventLedgerInstance lazily creates the shared notification ledger. The
// HTTP loopback door (server_http_routing.go) builds its phone-event handler
// per request, so this can run concurrently — sync.Once ensures every ingest
// records into one ledger rather than racing separate instances into being.
func (s *Server) phoneEventLedgerInstance() *phoneevents.Ledger {
	s.phoneEventLedgerOnce.Do(func() {
		s.phoneEventLedger = phoneevents.NewLedger(
			filepath.Join(config.ResolveStateDir(), phoneevents.LedgerDirname), s.logger,
		)
	})
	return s.phoneEventLedger
}

// siteVisitOnLocation lazily builds the site-visit recorder and returns its
// location callback for phoneevents.Config.OnLocationPlace. nil wiki store ⇒
// nil callback (site-visit recording off). Guarded by sync.Once: the HTTP
// ingest door constructs its handler per request, so two concurrent location
// updates must share the one recorder (separate recorders would each load empty
// dedup state and double-log the same visit).
func (s *Server) siteVisitOnLocation() func(string) {
	if s.wikiStore == nil {
		return nil
	}
	s.siteVisitRecorderOnce.Do(func() {
		s.siteVisitRecorder = wikiwork.NewSiteVisitRecorder(
			s.wikiStore, s.logger,
			filepath.Join(config.ResolveStateDir(), wikiwork.SiteVisitStateFile),
		)
	})
	return s.siteVisitRecorder.RecordFromLocationPayload
}

// phoneActionConfirmWait bounds how long a phone_write dispatch waits for the
// app's execution report before degrading to dispatched-unconfirmed
// (toolbind.ErrPhoneActionUnconfirmed). Foreground SSE execution reports back
// well under a second; the cap only bites on a backgrounded phone (Doze) or
// an app build that predates result reporting — both degrade to the old
// fire-and-forget semantics instead of blocking the turn.
const phoneActionConfirmWait = 5 * time.Second

// phoneActionResult is the app's execution report for one dispatched action,
// arriving as a miniapp.event.ingest event (type=phone_action_result) whose
// text is this JSON. OK mirrors the app's executePhoneAction boolean: the
// intent (or in-app service call) actually launched.
type phoneActionResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// phoneActionWaiter tracks one in-flight dispatch: the result channel plus the
// fan-out size, because the push hub delivers the frame to EVERY connected
// mobile client — including the headless verification harness, whose desktop
// build reports ok=false for every intent action. Success-biased aggregation:
// any ok=true resolves immediately; an ok=false only resolves once every
// fanned-out subscriber has reported failure, so the harness's instant "false"
// cannot mask the real phone's success.
//
// Verdict semantics under broadcast (deliberate): success = "at least one
// connected mobile client launched it", failure = "every connected mobile
// client failed". Reports carry no executor identity — the push hub knows
// only mobile/desktop, not which subscriber is the actual phone — so a
// non-phone client's success on the few actions it can perform (e.g. the
// harness's desktop open_url) confirms a launch the user's phone never saw.
// Targeting a single executor needs a device-identity layer in the push hub;
// follow-up, out of scope here. Single-operator deployment keeps the window
// narrow (a second mobile client is normally only the harness, whose intent
// actions all report false).
type phoneActionWaiter struct {
	ch       chan phoneActionResult
	expected int // mobile subscribers the frame fanned out to (>= 1)
	fails    int // ok=false reports received so far
}

// phoneActionAwaiter correlates dispatched phone actions with the app's
// result reports. One independent mutex, never held while blocking; waiter
// channels are buffered(1) and the map entry is removed before the (sole)
// send, so resolve never blocks and a late report after the dispatch timed
// out is discarded silently (logged by the ingest side).
type phoneActionAwaiter struct {
	mu sync.Mutex
	m  map[string]*phoneActionWaiter
}

func newPhoneActionAwaiter() *phoneActionAwaiter {
	return &phoneActionAwaiter{m: make(map[string]*phoneActionWaiter)}
}

// register creates the waiter for a dispatch id fanned out to `expected`
// subscribers. The caller MUST `defer drop(id)` so an unresolved waiter
// cannot leak past its dispatch.
func (a *phoneActionAwaiter) register(id string, expected int) <-chan phoneActionResult {
	if expected < 1 {
		expected = 1
	}
	w := &phoneActionWaiter{ch: make(chan phoneActionResult, 1), expected: expected}
	a.mu.Lock()
	a.m[id] = w
	a.mu.Unlock()
	return w.ch
}

// drop removes a waiter without resolving it (timeout / turn canceled).
// Idempotent — resolve may have already deleted the entry.
func (a *phoneActionAwaiter) drop(id string) {
	a.mu.Lock()
	delete(a.m, id)
	a.mu.Unlock()
}

// resolve feeds one report into the matching waiter. Returns false when no
// waiter holds the id (report arrived after the wait window, or the gateway
// restarted between dispatch and report). ok=true resolves the dispatch
// immediately; ok=false resolves it only when every fanned-out subscriber has
// reported failure — otherwise the report is absorbed and the dispatch keeps
// waiting for a possible success from another subscriber.
func (a *phoneActionAwaiter) resolve(res phoneActionResult) bool {
	a.mu.Lock()
	w, ok := a.m[res.ID]
	if !ok {
		a.mu.Unlock()
		return false
	}
	if !res.OK {
		w.fails++
		if w.fails < w.expected {
			a.mu.Unlock()
			return true // absorbed; another subscriber may still succeed
		}
	}
	delete(a.m, res.ID)
	a.mu.Unlock()
	w.ch <- res // buffered(1), sole sender after map removal — never blocks
	return true
}

// newPhoneActionID returns a random correlation id. Deliberately NOT the
// shared shortid counter: that one is sequential (any local process could
// spray confirmations at the unauthenticated loopback ingest door) and wraps
// at 10000 across all prefixes (a wrap collision would overwrite an unrelated
// in-flight waiter). 64 random bits close both.
func newPhoneActionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails in practice; a nanosecond stamp keeps the
		// dispatch functional (uniqueness, not unguessability) if it ever does.
		return fmt.Sprintf("pa-%d", time.Now().UnixNano())
	}
	return "pa-" + hex.EncodeToString(b)
}

// dispatchPhoneAction delivers a validated phone action command to the native
// app over the existing SSE push channel for in-app Intent execution. It is
// wired into the phone_write tool as its PhoneActionFunc. The command travels
// in the frame's Data (action + args) under Kind=pushKindPhoneAction, with the
// dispatch id in Ref. SSE is the ONLY transport — the FCM data fallback does
// not carry phone_action frames (FcmService renders title/body notifications
// only), so a fully backgrounded phone fails the mobile-subscriber gate below
// instead of executing late.
//
// Errors when no MOBILE app is connected so the agent learns the action could
// not be executed, rather than assuming a silent SSH-style success. The gate
// counts mobile subscribers specifically: the desktop client shares the same
// push hub but has no phone-action executor, so a desktop-only connection must
// not read as "dispatched" (phone_read's sync_state retry guidance would loop
// misleadingly).
//
// Result round-trip: after publishing, the dispatch waits up to
// phoneActionConfirmWait for the app's phone_action_result report (correlated
// by Ref id) — nil only on a confirmed launch, a real error once every
// fanned-out subscriber reported failure, toolbind.ErrPhoneActionUnconfirmed when
// no verdict arrives (fail-open: the action may still execute late, so the
// tool tells the agent not to retry). sync_state stays fire-and-forget — it
// has no in-app execution result; its effect arrives as
// location_update/usage_update events.
func (s *Server) dispatchPhoneAction(ctx context.Context, action string, args map[string]string) error {
	if s.pushHub == nil {
		return fmt.Errorf("no mobile app connected to execute the phone action")
	}
	fanout := s.pushHub.MobileSubscriberCount()
	if fanout == 0 {
		return fmt.Errorf("no mobile app connected to execute the phone action")
	}
	data := make(map[string]string, len(args)+1)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action

	if action == "sync_state" {
		s.pushHub.Publish(nativepush.Event{
			Kind:  nativepush.PushKindPhoneAction,
			Title: "phone action",
			Body:  action,
			Data:  data,
		})
		s.logger.Info("phone action dispatched to app", "action", action)
		return nil
	}

	id := newPhoneActionID()
	result := s.phoneActions.register(id, fanout)
	defer s.phoneActions.drop(id)

	s.pushHub.Publish(nativepush.Event{
		Kind:  nativepush.PushKindPhoneAction,
		Title: "phone action",
		Body:  action,
		Ref:   id,
		Data:  data,
	})
	s.logger.Info("phone action dispatched to app", "action", action, "id", id, "fanout", fanout)

	finish := func(res phoneActionResult) error {
		if !res.OK {
			msg := strings.TrimSpace(res.Error)
			if msg == "" {
				msg = "the app could not launch it (no handler, missing args, or unsupported on this platform)"
			}
			// The agent surfaces this to the user in its reply, so Warn (recoverable,
			// user-observable through the tool error), not a buried Info.
			s.logger.Warn("phone action failed on device", "action", action, "id", id, "detail", msg)
			return fmt.Errorf("phone action %q failed on the device: %s", action, msg)
		}
		s.logger.Info("phone action confirmed by app", "action", action, "id", id)
		s.recordOutgoingReply(action, args)
		return nil
	}

	timer := time.NewTimer(phoneActionConfirmWait)
	defer timer.Stop()
	select {
	case res := <-result:
		return finish(res)
	case <-ctx.Done():
		return fmt.Errorf("%w: turn ended while waiting", toolbind.ErrPhoneActionUnconfirmed)
	case <-timer.C:
		// Boundary drain: resolve may have delivered right as the timer fired;
		// without this a reported verdict could be masked as benign-unconfirmed
		// (worst case an actual failure downgraded to "may still run — don't
		// retry", the semantic inverse).
		select {
		case res := <-result:
			return finish(res)
		default:
		}
		s.logger.Info("phone action result not reported in time", "action", action, "id", id,
			"wait", phoneActionConfirmWait)
		return toolbind.ErrPhoneActionUnconfirmed
	}
}

// recordOutgoingReply appends a confirmed reply to the phone-event ledger in the
// same shape the notification listener uses, so phone_read("messages") shows the
// conversation with both sides.
//
// Outgoing messages are otherwise invisible: an app posts no notification for
// what the user sends, so the ledger only ever saw the incoming half. A reply
// Deneb itself sent is the one outgoing message it can know for certain — it
// wrote it — and recording only confirmed sends keeps the thread honest rather
// than showing an intent that never landed.
func (s *Server) recordOutgoingReply(action string, args map[string]string) {
	ledger := s.phoneEventLedgerInstance()
	if action != "reply" || ledger == nil {
		return
	}
	room := strings.TrimSpace(args["room"])
	text := strings.TrimSpace(args["text"])
	if room == "" || text == "" {
		return
	}
	ledger.Append("notification", "카카오톡",
		"대화방: "+room+"\n메시지:\n- 나(데네브): "+text)
}
