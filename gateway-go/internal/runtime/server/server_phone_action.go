package server

import (
	"context"
	"fmt"
)

// dispatchPhoneAction delivers a validated phone action to the native app for
// in-app Intent execution. It is wired into the phone_write tool as its
// PhoneActionFunc. Two delivery paths, tried in order:
//
//   - P1 (live SSE): a connected app holds the events stream → publish the
//     command on it (Kind=pushKindPhoneAction, Data=action+args) for immediate
//     in-app execution.
//   - P2 (FCM fallback): no live subscriber (app closed / Doze) → push the
//     command as an FCM notification the user taps to execute. A notification tap
//     is the only OS-sanctioned way to start an Intent from the background, and
//     it doubles as the user's consent to the action.
//
// Errors only when neither path is available (no SSE and FCM not configured) so
// the agent learns the action could not be delivered rather than assuming a
// silent SSH-style success.
func (s *Server) dispatchPhoneAction(_ context.Context, action string, args map[string]string) error {
	// P1: a live app executes immediately over the events SSE stream.
	if s.pushHub != nil && s.pushHub.subscriberCount() > 0 {
		s.pushHub.publish(clientPushEvent{
			Kind:  pushKindPhoneAction,
			Title: "phone action",
			Body:  action,
			Data:  phoneActionData(action, args),
		})
		s.logger.Info("phone action dispatched to app (sse)", "action", action)
		return nil
	}

	// P2: no live subscriber → FCM. The app surfaces a tray notification; tapping
	// it executes the Intent (a user-initiated start = OS-allowed + user consent).
	if s.pushNotifier != nil {
		title, body := phoneActionNotice(action, args)
		data := phoneActionData(action, args)
		data["kind"] = pushKindPhoneAction // app routes this apart from a proactive report
		s.pushNotifier.DeliverPhoneAction(title, body, data)
		s.logger.Info("phone action deferred to FCM notification (no live app)", "action", action)
		return nil
	}

	return fmt.Errorf("no native app connected and FCM fallback not configured to execute the phone action")
}

// phoneActionData builds the command payload the app reads to execute an Intent:
// the action's args plus the action name. Shared by the SSE and FCM paths (the
// FCM path additionally tags kind=pushKindPhoneAction for receipt routing).
func phoneActionData(action string, args map[string]string) map[string]string {
	data := make(map[string]string, len(args)+2)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action
	return data
}

// phoneActionNotice is the Korean tray title/body for the FCM fallback — what the
// user sees before tapping to execute. Body carries the concrete target so the
// action is reviewable at a glance (which url, which number, which recipient).
func phoneActionNotice(action string, args map[string]string) (title, body string) {
	switch action {
	case "open_url":
		return "링크 열기", args["url"]
	case "open_app":
		return "앱 열기", args["package"]
	case "share":
		return "공유", args["text"]
	case "message":
		if to := args["to"]; to != "" {
			return "메시지 보내기", to + " · " + args["text"]
		}
		return "메시지 보내기", args["text"]
	case "dial":
		return "전화 걸기", args["number"]
	case "photo":
		return "카메라 열기", "탭하면 카메라가 열립니다"
	default:
		return "폰 동작", action
	}
}
