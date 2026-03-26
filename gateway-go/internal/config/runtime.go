package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// GatewayRuntimeConfig holds the fully resolved gateway runtime settings
// after applying CLI overrides, environment variables, and validation constraints.
// This mirrors src/gateway/server-runtime-config.ts GatewayRuntimeConfig.
type GatewayRuntimeConfig struct {
	BindHost                      string
	Port                          int
	ControlUIEnabled              bool
	ControlUIBasePath             string
	ControlUIRoot                 string
	OpenAIChatCompletionsEnabled  bool
	OpenAIChatCompletionsConfig   *GatewayHTTPChatCompletionsConfig
	OpenResponsesEnabled          bool
	OpenResponsesConfig           *GatewayHTTPResponsesConfig
	StrictTransportSecurityHeader string
	ResolvedAuth                  ResolvedGatewayAuth
	AuthMode                      string
	TailscaleConfig               GatewayTailscaleConfig
	TailscaleMode                 string // "off" | "serve" | "funnel"
	CanvasHostEnabled             bool
	TrustedProxies                []string
	ChannelHealthCheckMinutes     int
	ChannelStaleEventThresholdMin int
	ChannelMaxRestartsPerHour     int
}

// RuntimeConfigParams are the inputs for resolving the runtime config.
type RuntimeConfigParams struct {
	Config            *DenebConfig
	Port              int
	Bind              string // Override bind mode.
	Host              string // Override bind host.
	ControlUIEnabled  *bool
	Auth              *ResolvedGatewayAuth
	TailscaleOverride *GatewayTailscaleConfig
}

// ResolveGatewayRuntimeConfig validates constraints and produces the final runtime config.
// This ports the logic from src/gateway/server-runtime-config.ts.
func ResolveGatewayRuntimeConfig(params RuntimeConfigParams) (*GatewayRuntimeConfig, error) {
	cfg := params.Config
	gw := cfg.Gateway
	if gw == nil {
		gw = &GatewayConfig{}
	}

	// Resolve bind mode and host.
	bindMode := params.Bind
	if bindMode == "" {
		bindMode = gw.Bind
	}
	if bindMode == "" {
		bindMode = "loopback"
	}

	bindHost := params.Host
	if bindHost == "" {
		var err error
		bindHost, err = resolveBindHost(bindMode, gw.CustomBindHost)
		if err != nil {
			return nil, err
		}
	}

	// Validate loopback constraint.
	if bindMode == "loopback" && !isLoopbackHost(bindHost) {
		return nil, fmt.Errorf(
			"gateway bind=loopback resolved to non-loopback host %s; refusing fallback to a network bind",
			bindHost,
		)
	}

	// Validate custom bind host.
	if bindMode == "custom" {
		customHost := strings.TrimSpace(gw.CustomBindHost)
		if customHost == "" {
			return nil, fmt.Errorf("gateway.bind=custom requires gateway.customBindHost")
		}
		if !isValidIPv4(customHost) {
			return nil, fmt.Errorf("gateway.bind=custom requires a valid IPv4 customBindHost (got %s)", customHost)
		}
		if bindHost != customHost {
			return nil, fmt.Errorf("gateway bind=custom requested %s but resolved %s; refusing fallback", customHost, bindHost)
		}
	}

	// Control UI.
	controlUIEnabled := true
	if params.ControlUIEnabled != nil {
		controlUIEnabled = *params.ControlUIEnabled
	} else if gw.ControlUI != nil && gw.ControlUI.Enabled != nil {
		controlUIEnabled = *gw.ControlUI.Enabled
	}

	controlUIBasePath := normalizeControlUIBasePath(gw.ControlUI)
	controlUIRoot := ""
	if gw.ControlUI != nil && strings.TrimSpace(gw.ControlUI.Root) != "" {
		controlUIRoot = strings.TrimSpace(gw.ControlUI.Root)
	}

	// HTTP endpoints.
	openAIChatCompletionsEnabled := false
	var openAIChatCompletionsConfig *GatewayHTTPChatCompletionsConfig
	if gw.HTTP != nil && gw.HTTP.Endpoints != nil && gw.HTTP.Endpoints.ChatCompletions != nil {
		cc := gw.HTTP.Endpoints.ChatCompletions
		openAIChatCompletionsConfig = cc
		if cc.Enabled != nil && *cc.Enabled {
			openAIChatCompletionsEnabled = true
		}
	}

	openResponsesEnabled := false
	var openResponsesConfig *GatewayHTTPResponsesConfig
	if gw.HTTP != nil && gw.HTTP.Endpoints != nil && gw.HTTP.Endpoints.Responses != nil {
		rc := gw.HTTP.Endpoints.Responses
		openResponsesConfig = rc
		if rc.Enabled != nil && *rc.Enabled {
			openResponsesEnabled = true
		}
	}

	// Strict-Transport-Security header.
	stsHeader := ""
	if gw.HTTP != nil && gw.HTTP.SecurityHeaders != nil && gw.HTTP.SecurityHeaders.StrictTransportSecurity != nil {
		v := strings.TrimSpace(*gw.HTTP.SecurityHeaders.StrictTransportSecurity)
		if v != "" && v != "false" {
			stsHeader = v
		}
	}

	// Tailscale.
	tailscaleCfg := GatewayTailscaleConfig{Mode: "off"}
	if gw.Tailscale != nil {
		tailscaleCfg = *gw.Tailscale
	}
	if params.TailscaleOverride != nil {
		tailscaleCfg = *mergeTailscaleConfig(&tailscaleCfg, params.TailscaleOverride)
	}
	tailscaleMode := tailscaleCfg.Mode
	if tailscaleMode == "" {
		tailscaleMode = "off"
	}

	// Resolved auth.
	resolvedAuth := ResolvedGatewayAuth{Mode: "token"}
	if params.Auth != nil {
		resolvedAuth = *params.Auth
	}
	authMode := resolvedAuth.Mode

	// ── Validation constraints ──

	// Tailscale funnel requires password auth.
	if tailscaleMode == "funnel" && authMode != "password" {
		return nil, fmt.Errorf(
			"tailscale funnel requires gateway auth mode=password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)",
		)
	}

	// Tailscale serve/funnel requires loopback bind.
	if tailscaleMode != "off" && !isLoopbackHost(bindHost) {
		return nil, fmt.Errorf("tailscale serve/funnel requires gateway bind=loopback (127.0.0.1)")
	}

	// Non-loopback requires auth.
	if !isLoopbackHost(bindHost) && !resolvedAuth.HasSharedSecret() && authMode != "trusted-proxy" {
		return nil, fmt.Errorf(
			"refusing to bind gateway to %s:%d without auth (set gateway.auth.token/password, or set DENEB_GATEWAY_TOKEN/DENEB_GATEWAY_PASSWORD)",
			bindHost, params.Port,
		)
	}

	// Non-loopback Control UI requires allowed origins.
	allowedOrigins := getControlUIAllowedOrigins(gw.ControlUI)
	dangerouslyAllowHostHeader := false
	if gw.ControlUI != nil && gw.ControlUI.DangerouslyAllowHostHeaderOriginFallback != nil {
		dangerouslyAllowHostHeader = *gw.ControlUI.DangerouslyAllowHostHeaderOriginFallback
	}
	if controlUIEnabled && !isLoopbackHost(bindHost) && len(allowedOrigins) == 0 && !dangerouslyAllowHostHeader {
		return nil, fmt.Errorf(
			"non-loopback Control UI requires gateway.controlUi.allowedOrigins (set explicit origins), " +
				"or set gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback=true",
		)
	}

	// Trusted-proxy auth requires trustedProxies.
	trustedProxies := gw.TrustedProxies
	if authMode == "trusted-proxy" {
		if len(trustedProxies) == 0 {
			return nil, fmt.Errorf(
				"gateway auth mode=trusted-proxy requires gateway.trustedProxies to be configured with at least one proxy IP",
			)
		}
		if isLoopbackHost(bindHost) {
			hasLoopback := isTrustedProxyAddress("127.0.0.1", trustedProxies) ||
				isTrustedProxyAddress("::1", trustedProxies)
			if !hasLoopback {
				return nil, fmt.Errorf(
					"gateway auth mode=trusted-proxy with bind=loopback requires gateway.trustedProxies to include 127.0.0.1, ::1, or a loopback CIDR",
				)
			}
		}
	}

	// Canvas host.
	canvasHostEnabled := true
	if os.Getenv("DENEB_SKIP_CANVAS_HOST") == "1" {
		canvasHostEnabled = false
	}
	if cfg.CanvasHost != nil && cfg.CanvasHost.Enabled != nil && !*cfg.CanvasHost.Enabled {
		canvasHostEnabled = false
	}

	// Channel health defaults.
	channelHealthCheck := 5
	if gw.ChannelHealthCheckMinutes != nil {
		channelHealthCheck = *gw.ChannelHealthCheckMinutes
	}
	channelStale := 30
	if gw.ChannelStaleEventThresholdMinutes != nil {
		channelStale = *gw.ChannelStaleEventThresholdMinutes
	}
	channelMaxRestarts := 10
	if gw.ChannelMaxRestartsPerHour != nil {
		channelMaxRestarts = *gw.ChannelMaxRestartsPerHour
	}

	return &GatewayRuntimeConfig{
		BindHost:                      bindHost,
		Port:                          params.Port,
		ControlUIEnabled:              controlUIEnabled,
		ControlUIBasePath:             controlUIBasePath,
		ControlUIRoot:                 controlUIRoot,
		OpenAIChatCompletionsEnabled:  openAIChatCompletionsEnabled,
		OpenAIChatCompletionsConfig:   openAIChatCompletionsConfig,
		OpenResponsesEnabled:          openResponsesEnabled,
		OpenResponsesConfig:           openResponsesConfig,
		StrictTransportSecurityHeader: stsHeader,
		ResolvedAuth:                  resolvedAuth,
		AuthMode:                      authMode,
		TailscaleConfig:               tailscaleCfg,
		TailscaleMode:                 tailscaleMode,
		CanvasHostEnabled:             canvasHostEnabled,
		TrustedProxies:                trustedProxies,
		ChannelHealthCheckMinutes:     channelHealthCheck,
		ChannelStaleEventThresholdMin: channelStale,
		ChannelMaxRestartsPerHour:     channelMaxRestarts,
	}, nil
}

// resolveBindHost maps a bind mode to an IP address.
func resolveBindHost(mode, customHost string) (string, error) {
	switch mode {
	case "loopback", "":
		return "127.0.0.1", nil
	case "lan", "all":
		return "0.0.0.0", nil
	case "auto":
		// Prefer loopback; this simplified version always returns loopback.
		// Full implementation would check if loopback is available.
		return "127.0.0.1", nil
	case "tailnet":
		// Try to find a Tailscale IP (100.64.0.0/10).
		if ip := findTailscaleIP(); ip != "" {
			return ip, nil
		}
		return "127.0.0.1", nil
	case "custom":
		host := strings.TrimSpace(customHost)
		if host == "" {
			return "", fmt.Errorf("gateway.bind=custom requires gateway.customBindHost")
		}
		return host, nil
	default:
		return "", fmt.Errorf("invalid bind mode: %s", mode)
	}
}

// isLoopbackHost checks if a host string is a loopback address.
func isLoopbackHost(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isValidIPv4 checks if a string is a valid IPv4 address.
func isValidIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// isTrustedProxyAddress checks if an address matches any trusted proxy entry.
// Supports exact IP match and CIDR notation.
func isTrustedProxyAddress(addr string, trustedProxies []string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	for _, proxy := range trustedProxies {
		if strings.Contains(proxy, "/") {
			_, network, err := net.ParseCIDR(proxy)
			if err == nil && network.Contains(ip) {
				return true
			}
		} else {
			proxyIP := net.ParseIP(proxy)
			if proxyIP != nil && proxyIP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// findTailscaleIP scans network interfaces for a Tailscale IP (100.64.0.0/10).
func findTailscaleIP() string {
	_, tsNet, _ := net.ParseCIDR("100.64.0.0/10")
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && tsNet.Contains(ip) {
				return ip.String()
			}
		}
	}
	return ""
}

// normalizeControlUIBasePath normalizes the control UI base path.
func normalizeControlUIBasePath(controlUI *GatewayControlUIConfig) string {
	if controlUI == nil || strings.TrimSpace(controlUI.BasePath) == "" {
		return "/"
	}
	bp := strings.TrimSpace(controlUI.BasePath)
	if !strings.HasPrefix(bp, "/") {
		bp = "/" + bp
	}
	bp = strings.TrimRight(bp, "/")
	if bp == "" {
		return "/"
	}
	return bp
}

// getControlUIAllowedOrigins returns the trimmed, non-empty allowed origins.
func getControlUIAllowedOrigins(controlUI *GatewayControlUIConfig) []string {
	if controlUI == nil || len(controlUI.AllowedOrigins) == 0 {
		return nil
	}
	result := make([]string, 0, len(controlUI.AllowedOrigins))
	for _, origin := range controlUI.AllowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
