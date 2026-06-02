package server

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// nativeClientUserID identifies the operator for standalone client-token
// sessions. It is a sentinel (not a real external user id) because the single
// operator has no configured external user id; downstream miniapp handlers only
// require a non-nil Identity/User, so a stable synthetic identity is sufficient.
const nativeClientUserID int64 = 1

// syntheticOperatorIdentity builds the Identity returned when a request
// authenticates via the standalone client token. It mirrors the shape miniapp
// handlers expect (non-nil User) so the rest of the RPC pipeline is unchanged.
func syntheticOperatorIdentity() *clientauth.Identity {
	return &clientauth.Identity{
		User: &clientauth.User{
			ID:        nativeClientUserID,
			FirstName: "Deneb Native Client",
		},
		AuthDate: time.Now(),
		ChatType: "private",
		Raw:      map[string]string{"auth": "client_token"},
	}
}
