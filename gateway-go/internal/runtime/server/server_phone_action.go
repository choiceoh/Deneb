package server

import (
	"context"
	"fmt"
)

// dispatchPhoneAction delivers a validated phone action command to the native
// app over the existing SSE push channel for in-app Intent execution. It is
// wired into the phone_write tool as its PhoneActionFunc. The command travels in
// the frame's Data (action + args) under Kind=pushKindPhoneAction.
//
// Errors when no app is connected so the agent learns the action could not be
// executed, rather than assuming a silent SSH-style success. Best-effort
// otherwise — the SSE hub drops frames for an asleep consumer (Doze); a phone
// action that must survive a closed app is out of P1 scope (FCM-data follow-up).
func (s *Server) dispatchPhoneAction(_ context.Context, action string, args map[string]string) error {
	if s.pushHub == nil || s.pushHub.subscriberCount() == 0 {
		return fmt.Errorf("no native app connected to execute the phone action")
	}
	data := make(map[string]string, len(args)+1)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action

	s.pushHub.publish(clientPushEvent{
		Kind:  pushKindPhoneAction,
		Title: "phone action",
		Body:  action,
		Data:  data,
	})
	s.logger.Info("phone action dispatched to app", "action", action)
	return nil
}
