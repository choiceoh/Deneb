package server

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/browserbridge"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
)

// phoneEventHandlerConfig is the shared Config for miniapp.event.ingest and
// POST /api/event/ingest — both doors must behave identically.
func (s *Server) phoneEventHandlerConfig() phoneevents.Config {
	return phoneevents.Config{
		ChatHandler:         s.chatHandler,
		JudgmentModel:       s.submainRoleIfConfigured(),
		Relay:               &s.proactiveRelay,
		ShutdownContext:     s.ShutdownCtx(),
		Logger:              s.logger,
		Ledger:              s.phoneEventLedgerInstance(),
		OnLocationPlace:     s.siteVisitOnLocation(),
		BrowserEnrich:       s.approvalBrowserEnrich,
		TriggerApprovalScan: s.triggerGroupwareRadarScan,
		ResolvePhoneAction: func(res phoneevents.ActionResult) bool {
			if s.phoneActions == nil {
				return false
			}
			return s.phoneActions.resolve(phoneActionResult{ID: res.ID, OK: res.OK, Error: res.Error})
		},
	}
}

// triggerGroupwareRadarScan runs one on-demand radar scan for a phone
// electronic-approval notification. Errors (radar unregistered — dev without
// credentials — or a reader failure) make the phone handler fall back to the
// plain notification relay, so approvals stay observable either way.
func (s *Server) triggerGroupwareRadarScan(ctx context.Context) error {
	radar := s.groupwareRadar
	if radar == nil {
		return errors.New("groupware radar unavailable")
	}
	return radar.TriggerScan(ctx)
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
		groupwareURL = "https://tsgw.topsolar.kr" //nolint:gosec // default groupware base URL, not a credential
	}
	return browserbridge.New(base, token).ApprovalEnrich(ctx, groupwareURL, source, text)
}

// submainRoleIfConfigured returns the "submain" role name when agents.submainModel
// is set, else "" (the caller then resolves to the main role as before). This is
// the single gate that keeps the autonomous lanes (phone-event, heartbeat) on the
// submain model only when the operator has configured one — an unconfigured deploy
// behaves exactly as before.
func (s *Server) submainRoleIfConfigured() string {
	if s.modelRegistry != nil && s.modelRegistry.FullModelID(modelrole.RoleSubmain) != "" {
		return string(modelrole.RoleSubmain)
	}
	return ""
}
