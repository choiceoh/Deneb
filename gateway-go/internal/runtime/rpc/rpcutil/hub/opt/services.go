// Package opt holds late-bound GatewayHub services so the hub root package
// keeps required-only domain fan-out under the soft boundary bar.
package opt

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
)

// Services are optional / late-bound hub dependencies set after NewGatewayHub.
type Services struct {
	WikiStore     *wiki.Store
	ContactsStore *contacts.Store
	Insights      *insights.Engine
}
