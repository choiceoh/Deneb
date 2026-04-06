package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
)

// Services holds the wired server and supporting service state.
type Services struct {
	Server *server.Server
}

// WireServices assembles the gateway server with all backing services.
func WireServices(addr string, rtCfg *config.GatewayRuntimeConfig, logger *slog.Logger, version string, useColor bool) (Services, error) {
	srv, err := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(version),
		server.WithConfig(rtCfg),
		server.WithLogColor(useColor),
	)
	if err != nil {
		return Services{}, fmt.Errorf("server init: %w", err)
	}

	return Services{
		Server: srv,
	}, nil
}
