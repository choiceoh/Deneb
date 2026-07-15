package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire/late"
)

// registerLateMethods registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func (s *Server) registerLateMethods(hub *rpcutil.GatewayHub) {
	ports := s.wirePorts()
	if s.Mail != nil {
		ports.Mail.WikiSink = s.Mail.MakeMailAnalysisWikiSink()
	}
	late.RegisterDomains(hub, ports)
}
