package bootstrap

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
)

// Flags holds all parsed CLI flags for the gateway process.
type Flags struct {
	ConfigPath string
	Port       int
	Bind       string
	Version    string
	PIDFile    string
	DaemonMode bool
	LogLevel   string
}

// ParseFlags parses CLI flags from os.Args and resolves the version string.
func ParseFlags(compiledVersion string) Flags {
	configPath := flag.String("config", "", "Path to deneb.json config file")
	port := flag.Int("port", 0, "Gateway server port (overrides config)")
	bind := flag.String("bind", "", "Bind address: 'loopback', 'lan', 'all', 'custom', 'tailnet' (overrides config)")
	version := flag.String("version", "", "Server version string (overrides built-in version)")
	pidFile := flag.String("pid-file", "", "Path to PID file for daemon mode")
	daemonMode := flag.Bool("daemon", false, "Run as daemon (write PID file, check for existing)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (overrides config)")
	flag.Parse()

	v := *version
	if v == "" {
		v = compiledVersion
	}

	return Flags{
		ConfigPath: *configPath,
		Port:       *port,
		Bind:       *bind,
		Version:    v,
		PIDFile:    *pidFile,
		DaemonMode: *daemonMode,
		LogLevel:   *logLevel,
	}
}

// ConfigResult holds outputs from the config bootstrap phase.
type ConfigResult struct {
	Bootstrap  *config.BootstrapResult
	RuntimeCfg *config.GatewayRuntimeConfig
	Port       int
	Addr       string
	CfgDir     string
}

// LoadConfig runs the config bootstrap phase: loads .env, reads config,
// resolves port and runtime settings (bind host, auth, validation).
func LoadConfig(flags Flags, earlyLogger *slog.Logger) (ConfigResult, error) {
	config.LoadDotenvFiles(earlyLogger)

	bs, err := config.BootstrapGatewayConfig(config.BootstrapOptions{
		ConfigPath: flags.ConfigPath,
		Persist:    true,
	})
	if err != nil {
		return ConfigResult{}, fmt.Errorf("config bootstrap failed: %w", err)
	}

	port := config.ResolveGatewayPort(&bs.Config)
	if flags.Port > 0 {
		port = flags.Port
	}

	rtCfg, err := config.ResolveGatewayRuntimeConfig(config.RuntimeConfigParams{
		Config: &bs.Config,
		Port:   port,
		Bind:   flags.Bind,
		Auth:   &bs.Auth,
	})
	if err != nil {
		return ConfigResult{}, fmt.Errorf("runtime config resolution failed: %w", err)
	}

	return ConfigResult{
		Bootstrap:  bs,
		RuntimeCfg: rtCfg,
		Port:       port,
		Addr:       fmt.Sprintf("%s:%d", rtCfg.BindHost, rtCfg.Port),
		CfgDir:     cfgDirFromBootstrap(bs),
	}, nil
}

func cfgDirFromBootstrap(bs *config.BootstrapResult) string {
	if bs.Snapshot != nil && bs.Snapshot.Path != "" {
		return filepath.Dir(bs.Snapshot.Path)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deneb")
	}
	return ""
}
