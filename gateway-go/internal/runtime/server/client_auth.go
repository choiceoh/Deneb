package server

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// nativeClientUserID identifies the operator for standalone client-token
// sessions. It is a sentinel (not a real Telegram user id) because the single
// operator has no configured Telegram user id; downstream miniapp handlers only
// require a non-nil InitData/User, so a stable synthetic identity is sufficient.
const nativeClientUserID int64 = 1

// syntheticOperatorInitData builds the InitData returned when a request
// authenticates via the standalone client token instead of Telegram initData.
// It mirrors the shape miniapp handlers expect (non-nil User) so the rest of the
// RPC pipeline is unchanged.
func syntheticOperatorInitData() *telegram.InitData {
	return &telegram.InitData{
		User: &telegram.WebAppUser{
			ID:        nativeClientUserID,
			FirstName: "Deneb Native Client",
		},
		AuthDate: time.Now(),
		ChatType: "private",
		Raw:      map[string]string{"auth": "client_token"},
	}
}
