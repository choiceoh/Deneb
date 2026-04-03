// Package bootstrap handles the gateway startup sequence in four discrete phases:
//  1. Config:    parse CLI flags, load .env, bootstrap config, resolve runtime settings.
//  2. Logging:   build the structured logger from config and flag overrides.
//  3. Services:  wire the gateway server, Gemini embedder, and Vega backend.
//  4. Lifecycle: manage daemon mode, signal handling, and graceful shutdown.
//
// The sole public entry point is [Run], called from main.
package bootstrap

import (
	"log/slog"
)

// Run executes the full gateway startup sequence and returns an OS exit code.
// It is the sole entry point called from main.
func Run(compiledVersion string) int {
	// Phase 1: config — parse flags, load .env, bootstrap config, resolve runtime settings.
	flags := ParseFlags(compiledVersion)
	earlyLogger := BuildEarlyLogger(flags.LogLevel)
	cfg, err := LoadConfig(flags, earlyLogger)
	if err != nil {
		earlyLogger.Error("configuration failed", "error", err)
		return 1
	}

	// Phase 2: logging — build full structured logger from config.
	log := BuildLogger(&cfg.Bootstrap.Config, flags.LogLevel)
	// Keep package-level slog users (slog.Default()) on the same handler format
	// as the gateway logger so all runtime lines share one console style.
	slog.SetDefault(log.Logger)

	if cfg.Bootstrap.GeneratedToken != "" {
		log.Logger.Info("gateway auth token auto-generated",
			"persisted", cfg.Bootstrap.PersistedGeneratedToken,
			"configPath", cfg.Bootstrap.Snapshot.Path,
		)
	}

	// Phase 3: services — wire server, Gemini embedder, Vega backend.
	svc, err := WireServices(cfg.Addr, cfg.RuntimeCfg, log.Logger, flags.Version, log.UseColor)
	if err != nil {
		log.Logger.Error("server initialization failed", "error", err)
		return 1
	}

	// Phase 4: lifecycle — daemon or foreground run with signal handling.
	if flags.DaemonMode || flags.PIDFile != "" {
		return RunDaemon(flags, cfg, svc, log)
	}
	return RunServer(flags, cfg, svc, log)
}
