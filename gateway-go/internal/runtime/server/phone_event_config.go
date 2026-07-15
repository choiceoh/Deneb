package server

import (
	"context"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
)

// phoneEventHandlerConfig is the shared Config for miniapp.event.ingest and
// POST /api/event/ingest — both doors must behave identically.
func (s *Server) phoneEventHandlerConfig() phoneevents.Config {
	return phoneevents.Config{
		ChatHandler:     s.chatHandler,
		Relay:           &s.proactiveRelay,
		ShutdownContext: s.ShutdownCtx(),
		Logger:          s.logger,
		Ledger:          s.phoneEventLedgerInstance(),
		OnLocationPlace: s.siteVisitOnLocation(),
		BrowserEnrich:   s.approvalBrowserEnrich,
		ResolvePhoneAction: func(res phoneevents.ActionResult) bool {
			if s.phoneActions == nil {
				return false
			}
			return s.phoneActions.resolve(phoneActionResult{ID: res.ID, OK: res.OK, Error: res.Error})
		},
	}
}

// approvalBrowserEnrich prefers srv4 Amaranth login (DENEB_GROUPWARE_USER/PASSWORD)
// via headless Playwright; falls back to the workstation Page Agent bridge
// (DENEB_BROWSER_*) when credentials are unset.
func (s *Server) approvalBrowserEnrich(ctx context.Context, source, text string) string {
	if cfg, ok := groupware.FromEnv(); ok {
		if body := groupware.ReadApproval(ctx, cfg, source, text); body != "" {
			return body
		}
		if s.logger != nil {
			s.logger.Info("phone-event groupware reader returned empty; trying browser bridge",
				"source", source)
		}
	}

	base := strings.TrimSpace(os.Getenv("DENEB_BROWSER_URL"))
	if base == "" {
		return ""
	}
	token := strings.TrimSpace(os.Getenv("DENEB_BROWSER_TOKEN"))
	groupwareURL := strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_URL"))
	if groupwareURL == "" {
		groupwareURL = "https://tsgw.topsolar.kr"
	}
	return runtimeops.ApprovalBrowserEnrich(ctx, base, token, groupwareURL, source, text)
}
