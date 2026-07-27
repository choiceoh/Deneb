// Package gatewayhttp owns the gateway's client-facing and integration-facing
// HTTP adapter wiring.
//
// Business handlers remain in their owning packages; this package only turns
// the server's narrow runtime capabilities into stable routes and middleware.
package gatewayhttp

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/sparkfleet"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/appupdate"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/fileapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/fleetapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/groupwareapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mcpapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
)

// MailAttachmentClient retrieves one attachment from the configured mail
// source. Keeping this port here prevents the composition root from depending
// on nativeapi's concrete handler contract.
type MailAttachmentClient interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// Config is the client HTTP surface's complete runtime contract.
type Config struct {
	Dispatcher                  *rpc.Dispatcher
	ChatHandler                 chatport.SyncStreamRunner
	PushHub                     *nativepush.Hub
	ShutdownContext             context.Context
	Logger                      *slog.Logger
	AttachmentFactory           func() (MailAttachmentClient, error)
	GroupwareAttachmentDownload groupwareapi.AttachmentDownload
	// TranslateThinking renders a finished turn's reasoning into Korean for the
	// SSE done frame. Optional; nil leaves it in the model's own language.
	TranslateThinking func(ctx context.Context, text string) (string, bool)
	Fleet             *sparkfleet.Client
	Version           string
}

// FleetAlertConfig is the narrow composition contract for SparkFleet's
// loopback webhook. Delivery and cooldown ownership remain outside the HTTP
// adapter; this package only binds them to the stable route.
type FleetAlertConfig struct {
	Gate    fleetapi.AlertGate
	Publish func(title, body string)
	Logger  *slog.Logger
}

// RegisterRoutes registers the native-client, app-update, fleet, and MCP
// adapters. The caller keeps ownership of domain-specific routes and the root
// fallback.
func RegisterRoutes(mux *http.ServeMux, cfg Config) {
	nativeHandler := func() *nativeapi.Handler {
		return nativeapi.New(nativeapi.Config{
			Dispatcher:        cfg.Dispatcher,
			ChatHandler:       cfg.ChatHandler,
			PushHub:           cfg.PushHub,
			ShutdownContext:   cfg.ShutdownContext,
			Logger:            cfg.Logger,
			AttachmentFactory: adaptAttachmentFactory(cfg.AttachmentFactory),
			TranslateThinking: cfg.TranslateThinking,
		})
	}
	groupwareHandler := groupwareapi.New(groupwareapi.Config{
		Download: cfg.GroupwareAttachmentDownload,
		Logger:   cfg.Logger,
	})

	mux.HandleFunc("POST /api/v1/miniapp/rpc", func(w http.ResponseWriter, r *http.Request) {
		nativeHandler().RPC(w, r)
	})
	mux.HandleFunc("POST /api/v1/miniapp/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		nativeHandler().ChatStream(w, r)
	})
	mux.HandleFunc("GET /api/v1/miniapp/events", func(w http.ResponseWriter, r *http.Request) {
		nativeHandler().Events(w, r)
	})
	mux.HandleFunc("GET /api/v1/miniapp/gmail/attachment", func(w http.ResponseWriter, r *http.Request) {
		nativeHandler().GmailAttachment(w, r)
	})
	mux.HandleFunc("GET /api/v1/miniapp/groupware/approval/attachment", func(w http.ResponseWriter, r *http.Request) {
		groupwareHandler.ApprovalAttachment(w, r)
	})
	fileHandler := fileapi.New(cfg.Logger)
	mux.HandleFunc("GET /api/v1/files/download", func(w http.ResponseWriter, r *http.Request) {
		fileHandler.Download(w, r)
	})

	if os.Getenv("DENEB_MCP_DISABLE") != "1" {
		mcpHandler := mcpapi.New(mcpapi.Config{
			Authenticate: nativeapi.Authenticator(cfg.Logger),
			Dispatcher:   cfg.Dispatcher,
			Version:      cfg.Version,
			Logger:       cfg.Logger,
		})
		mux.Handle("/mcp", mcpHandler)
	}

	appUpdateHandler := appupdate.New(cfg.Logger)
	mux.HandleFunc("GET /api/v1/app/update/manifest", appUpdateHandler.Manifest)
	mux.HandleFunc("GET /api/v1/app/update/download", appUpdateHandler.Download)
	mux.Handle("/api/v1/fleet/", fleetapi.New(cfg.Fleet, cfg.Logger))

	registerMethodNotAllowed(
		mux,
		"/api/v1/miniapp/rpc",
		"/api/v1/miniapp/chat/stream",
		"/api/v1/miniapp/events",
		"/api/v1/miniapp/gmail/attachment",
		"/api/v1/miniapp/groupware/approval/attachment",
		"/api/v1/files/download",
	)
}

// RegisterFleetAlertRoute binds the SparkFleet webhook at its historical
// registration point. Keeping this separate from RegisterRoutes lets the
// server preserve the existing eval → Fleet hook → observatory route order.
func RegisterFleetAlertRoute(mux *http.ServeMux, cfg FleetAlertConfig) {
	mux.Handle("POST /api/hooks/fleet", fleetapi.NewAlertHook(fleetapi.AlertHookConfig{
		Gate:    cfg.Gate,
		Publish: cfg.Publish,
		Logger:  cfg.Logger,
	}))
}

// WithCORS lets browser clients reach the token-authenticated client surface.
// Origin-less native clients pass through unchanged.
func WithCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", nativeapi.ClientTokenHeader+", "+nativeapi.ClientKindHeader+", Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func adaptAttachmentFactory(factory func() (MailAttachmentClient, error)) func() (nativeapi.MailAttachmentClient, error) {
	if factory == nil {
		return nil
	}
	return func() (nativeapi.MailAttachmentClient, error) {
		return factory()
	}
}

func registerMethodNotAllowed(mux *http.ServeMux, patterns ...string) {
	for _, pattern := range patterns {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		})
	}
}
