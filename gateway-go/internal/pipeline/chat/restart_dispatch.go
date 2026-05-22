// restart_dispatch.go — /restart slash command dispatcher.
//
// Restarts the gateway from inside Telegram without pulling or rebuilding —
// useful for picking up a config change or clearing a wedged state.
//
// Shape:
//
//	/restart           → guidance: explains the restart and asks to confirm
//	/restart 확인       → restart now (SIGUSR1 → graceful shutdown → relaunch)
//	/restart confirm   → English alias for the confirm path
//
// Korean alias: /재시작 routes here too (see ParseSlashCommand).
//
// Restart reuses the same mechanism as the /update execute path: SIGUSR1 →
// bootstrap.ExitCodeRestart (75) → the supervising wrapper relaunches the
// gateway. The confirm-word parsing (normalizeConfirmArg) and the signal
// helper (signalGatewayRestart) are shared with update_dispatch.go.

package chat

import "strings"

// handleRestartCommand parses the /restart argument and either explains what
// a restart does or sends the restart signal. Spawned in a goroutine by the
// dispatcher so the reply is delivered before graceful shutdown begins.
func (h *Handler) handleRestartCommand(delivery *DeliveryContext, rawArgs string) {
	defer func() {
		if r := recover(); r != nil && h.logger != nil {
			h.logger.Error("panic in /restart command handler", "panic", r)
		}
	}()

	switch normalizeConfirmArg(rawArgs) {
	case confirmIntentBare:
		h.deliverSlashResponse(delivery,
			"♻️ 게이트웨이를 재시작하면 진행 중인 작업이 중단됩니다.\n"+
				"재시작하려면 `/restart 확인`을 입력하세요.")

	case confirmIntentYes:
		h.logger.Info("restart: requested via /restart slash command")
		// Deliver the notice before signalling — once SIGUSR1 lands the
		// gateway begins graceful shutdown and may not be able to reply.
		h.deliverSlashResponse(delivery, "♻️ 게이트웨이를 재시작합니다 — 몇 초 후 다시 사용할 수 있습니다.")
		if err := signalGatewayRestart(); err != nil {
			h.logger.Error("restart: signal failed", "error", err)
			h.deliverSlashResponse(delivery, "⚠️ 재시작 신호 전송에 실패했습니다. 게이트웨이를 수동으로 재시작해 주세요.")
		}

	default:
		h.deliverSlashResponse(delivery, strings.Join([]string{
			"사용법:",
			"  /restart — 재시작 안내",
			"  /restart 확인 — 게이트웨이 재시작",
		}, "\n"))
	}
}
