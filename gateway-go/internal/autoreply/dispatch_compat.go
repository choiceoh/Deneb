// dispatch_compat.go — temporary re-exports from the autoreply/dispatch subpackage.
// TODO: Remove after all callers are updated to import autoreply/dispatch directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/dispatch"

// Type aliases — dispatcher.go
type ReplyDispatcher = dispatch.ReplyDispatcher

// Function re-exports — dispatcher.go
var NewReplyDispatcher = dispatch.NewReplyDispatcher

// Function re-exports — route_reply.go
var RouteReply = dispatch.RouteReply
