package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// phoneActionConfirmWait bounds how long a phone_write dispatch waits for the
// app's execution report before degrading to dispatched-unconfirmed
// (tools.ErrPhoneActionUnconfirmed). Foreground SSE execution reports back
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

// phoneActionAwaiter correlates dispatched phone actions with the app's
// result reports. One independent mutex, never held while blocking; channels
// are buffered(1) and removed from the map on resolve OR drop, so a late
// report after the dispatch timed out is discarded silently (logged by the
// ingest side) and resolve never blocks.
type phoneActionAwaiter struct {
	mu sync.Mutex
	m  map[string]chan phoneActionResult
}

func newPhoneActionAwaiter() *phoneActionAwaiter {
	return &phoneActionAwaiter{m: make(map[string]chan phoneActionResult)}
}

// register creates the waiter channel for a dispatch id. The caller MUST
// `defer drop(id)` so an unresolved waiter cannot leak past its dispatch.
func (a *phoneActionAwaiter) register(id string) <-chan phoneActionResult {
	ch := make(chan phoneActionResult, 1)
	a.mu.Lock()
	a.m[id] = ch
	a.mu.Unlock()
	return ch
}

// drop removes a waiter without resolving it (timeout / turn canceled).
// Idempotent — resolve may have already deleted the entry.
func (a *phoneActionAwaiter) drop(id string) {
	a.mu.Lock()
	delete(a.m, id)
	a.mu.Unlock()
}

// resolve delivers the app's report to the waiting dispatch. Returns false
// when no waiter holds the id (report arrived after the wait window, or the
// gateway restarted between dispatch and report).
func (a *phoneActionAwaiter) resolve(res phoneActionResult) bool {
	a.mu.Lock()
	ch, ok := a.m[res.ID]
	if ok {
		delete(a.m, res.ID)
	}
	a.mu.Unlock()
	if !ok {
		return false
	}
	ch <- res // buffered(1), sole sender after map removal — never blocks
	return true
}

// dispatchPhoneAction delivers a validated phone action command to the native
// app over the existing SSE push channel for in-app Intent execution. It is
// wired into the phone_write tool as its PhoneActionFunc. The command travels
// in the frame's Data (action + args) under Kind=pushKindPhoneAction, with the
// dispatch id in Ref.
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
// by Ref id) — nil only on a confirmed launch, a real error on a reported
// failure, tools.ErrPhoneActionUnconfirmed when no report arrives (fail-open:
// the action may still execute late, so the tool tells the agent not to
// retry). sync_state stays fire-and-forget — it has no in-app execution
// result; its effect arrives as location_update/usage_update events.
func (s *Server) dispatchPhoneAction(ctx context.Context, action string, args map[string]string) error {
	if s.pushHub == nil || s.pushHub.mobileSubscriberCount() == 0 {
		return fmt.Errorf("no mobile app connected to execute the phone action")
	}
	data := make(map[string]string, len(args)+1)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action

	if action == "sync_state" {
		s.pushHub.publish(clientPushEvent{
			Kind:  pushKindPhoneAction,
			Title: "phone action",
			Body:  action,
			Data:  data,
		})
		s.logger.Info("phone action dispatched to app", "action", action)
		return nil
	}

	id := shortid.New("pa")
	result := s.phoneActions.register(id)
	defer s.phoneActions.drop(id)

	s.pushHub.publish(clientPushEvent{
		Kind:  pushKindPhoneAction,
		Title: "phone action",
		Body:  action,
		Ref:   id,
		Data:  data,
	})
	s.logger.Info("phone action dispatched to app", "action", action, "id", id)

	timer := time.NewTimer(phoneActionConfirmWait)
	defer timer.Stop()
	select {
	case res := <-result:
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
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: turn ended while waiting", tools.ErrPhoneActionUnconfirmed)
	case <-timer.C:
		s.logger.Info("phone action result not reported in time", "action", action, "id", id,
			"wait", phoneActionConfirmWait)
		return tools.ErrPhoneActionUnconfirmed
	}
}
