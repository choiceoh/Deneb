package config

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// GatewayRuntimeConfig holds the fully resolved gateway runtime settings
// after applying CLI overrides, environment variables, and validation constraints.
// This mirrors src/gateway/server-runtime-config.ts GatewayRuntimeConfig.
type GatewayRuntimeConfig struct {
	BindHost          string
	Port              int
	ControlUIEnabled  bool
	ControlUIBasePath string
	ControlUIRoot     string
	ResolvedAuth      ResolvedGatewayAuth
	AuthMode          string
	TailscaleConfig   GatewayTailscaleConfig
	TailscaleMode     string // "off" | "serve" | "funnel"
	TrustedProxies    []string
	ReloadConfig      GatewayReloadConfig
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
	Logger            *slog.Logger
}

// ResolveGatewayRuntimeConfig validates constraints and produces the final runtime config.
// This ports the logic from src/gateway/server-runtime-config.ts.
func ResolveGatewayRuntimeConfig(params RuntimeConfigParams) (*GatewayRuntimeConfig, error) {
	sources := selectGatewayRuntimeSources(params)
	inputs := applyGatewayRuntimeDefaults(sources)
	decision, err := deriveGatewayRuntimeDecision(inputs)
	if err != nil {
		return nil, err
	}
	if err := validateGatewayRuntimeDecision(decision); err != nil {
		return nil, err
	}
	return decision.runtimeConfig(), nil
}

// gatewayRuntimeSources captures where every setting can come from without
// applying precedence or defaults. Keeping this raw layer makes CLI overrides
// distinguishable from values already loaded from deneb.json.
type gatewayRuntimeSources struct {
	gateway                  *GatewayConfig
	port                     int
	bindOverride             string
	hostOverride             string
	controlUIEnabledOverride *bool
	authOverride             *ResolvedGatewayAuth
	tailscaleOverride        *GatewayTailscaleConfig
	logger                   *slog.Logger
}

func selectGatewayRuntimeSources(params RuntimeConfigParams) gatewayRuntimeSources {
	gateway := params.Config.Gateway
	if gateway == nil {
		gateway = &GatewayConfig{}
	}
	return gatewayRuntimeSources{
		gateway:                  gateway,
		port:                     params.Port,
		bindOverride:             params.Bind,
		hostOverride:             params.Host,
		controlUIEnabledOverride: params.ControlUIEnabled,
		authOverride:             params.Auth,
		tailscaleOverride:        params.TailscaleOverride,
		logger:                   params.Logger,
	}
}

// gatewayRuntimeInputs contains concrete values after override precedence and
// runtime-owned defaults have been applied, but before any derived decisions.
type gatewayRuntimeInputs struct {
	port             int
	bindMode         string
	bindHostOverride string
	customBindHost   string
	controlUI        *GatewayControlUIConfig
	controlUIEnabled bool
	resolvedAuth     ResolvedGatewayAuth
	tailscaleConfig  GatewayTailscaleConfig
	trustedProxies   []string
	reloadConfig     GatewayReloadConfig
	logger           *slog.Logger
}

func applyGatewayRuntimeDefaults(sources gatewayRuntimeSources) gatewayRuntimeInputs {
	return gatewayRuntimeInputs{
		port:             sources.port,
		bindMode:         selectGatewayBindMode(sources.bindOverride, sources.gateway.Bind),
		bindHostOverride: sources.hostOverride,
		customBindHost:   sources.gateway.CustomBindHost,
		controlUI:        sources.gateway.ControlUI,
		controlUIEnabled: selectControlUIEnabled(sources.controlUIEnabledOverride, sources.gateway.ControlUI),
		resolvedAuth:     selectResolvedGatewayAuth(sources.authOverride),
		tailscaleConfig: selectGatewayTailscaleConfig(
			sources.gateway.Tailscale,
			sources.tailscaleOverride,
		),
		trustedProxies: sources.gateway.TrustedProxies,
		reloadConfig:   selectGatewayReloadConfig(sources.gateway.Reload),
		logger:         selectRuntimeLogger(sources.logger),
	}
}

func selectGatewayBindMode(override, configured string) string {
	if override != "" {
		return override
	}
	if configured != "" {
		return configured
	}
	return BindLoopback
}

func selectControlUIEnabled(override *bool, controlUI *GatewayControlUIConfig) bool {
	if override != nil {
		return *override
	}
	if controlUI != nil && controlUI.Enabled != nil {
		return *controlUI.Enabled
	}
	return true
}

func selectResolvedGatewayAuth(override *ResolvedGatewayAuth) ResolvedGatewayAuth {
	if override != nil {
		return *override
	}
	return ResolvedGatewayAuth{Mode: AuthModeToken}
}

func selectGatewayTailscaleConfig(configured, override *GatewayTailscaleConfig) GatewayTailscaleConfig {
	selected := GatewayTailscaleConfig{Mode: TailscaleOff}
	if configured != nil {
		selected = *configured
	}
	if override != nil {
		selected = *mergeTailscaleConfig(&selected, override)
	}
	return selected
}

func selectGatewayReloadConfig(configured *GatewayReloadConfig) GatewayReloadConfig {
	if configured != nil {
		return *configured
	}
	return GatewayReloadConfig{Mode: ReloadHybrid}
}

func selectRuntimeLogger(configured *slog.Logger) *slog.Logger {
	if configured != nil {
		return configured
	}
	return slog.Default()
}

// gatewayRuntimeDecision stores the derived values together with the minimal
// source context needed to validate that the decisions are safe.
type gatewayRuntimeDecision struct {
	port                       int
	bindMode                   string
	bindHost                   string
	customBindHost             string
	controlUIEnabled           bool
	controlUIBasePath          string
	controlUIRoot              string
	controlUIAllowedOrigins    []string
	dangerouslyAllowHostHeader bool
	resolvedAuth               ResolvedGatewayAuth
	authMode                   string
	tailscaleConfig            GatewayTailscaleConfig
	tailscaleMode              string
	trustedProxies             []string
	reloadConfig               GatewayReloadConfig
}

func deriveGatewayRuntimeDecision(inputs gatewayRuntimeInputs) (gatewayRuntimeDecision, error) {
	bindHost, err := deriveGatewayBindHost(inputs)
	if err != nil {
		return gatewayRuntimeDecision{}, err
	}
	return gatewayRuntimeDecision{
		port:                       inputs.port,
		bindMode:                   inputs.bindMode,
		bindHost:                   bindHost,
		customBindHost:             strings.TrimSpace(inputs.customBindHost),
		controlUIEnabled:           inputs.controlUIEnabled,
		controlUIBasePath:          normalizeControlUIBasePath(inputs.controlUI),
		controlUIRoot:              controlUIRoot(inputs.controlUI),
		controlUIAllowedOrigins:    getControlUIAllowedOrigins(inputs.controlUI),
		dangerouslyAllowHostHeader: dangerouslyAllowHostHeaderOriginFallback(inputs.controlUI),
		resolvedAuth:               inputs.resolvedAuth,
		authMode:                   inputs.resolvedAuth.Mode,
		tailscaleConfig:            inputs.tailscaleConfig,
		tailscaleMode:              effectiveTailscaleMode(inputs.tailscaleConfig.Mode),
		trustedProxies:             inputs.trustedProxies,
		reloadConfig:               inputs.reloadConfig,
	}, nil
}

func deriveGatewayBindHost(inputs gatewayRuntimeInputs) (string, error) {
	if inputs.bindHostOverride != "" {
		return inputs.bindHostOverride, nil
	}
	return resolveBindHost(inputs.bindMode, inputs.customBindHost, inputs.logger)
}

func controlUIRoot(controlUI *GatewayControlUIConfig) string {
	if controlUI == nil {
		return ""
	}
	return strings.TrimSpace(controlUI.Root)
}

func dangerouslyAllowHostHeaderOriginFallback(controlUI *GatewayControlUIConfig) bool {
	if controlUI == nil || controlUI.DangerouslyAllowHostHeaderOriginFallback == nil {
		return false
	}
	return *controlUI.DangerouslyAllowHostHeaderOriginFallback
}

func effectiveTailscaleMode(mode string) string {
	if mode == "" {
		return TailscaleOff
	}
	return mode
}

func validateGatewayRuntimeDecision(decision gatewayRuntimeDecision) error {
	if err := validateGatewayBindDecision(decision); err != nil {
		return err
	}
	if err := validateGatewayTailscaleDecision(decision); err != nil {
		return err
	}
	if err := validateGatewayNetworkExposure(decision); err != nil {
		return err
	}
	return validateGatewayTrustedProxyDecision(decision)
}

func validateGatewayBindDecision(decision gatewayRuntimeDecision) error {
	if decision.bindMode == BindLoopback && !isLoopbackHost(decision.bindHost) {
		return fmt.Errorf(
			"gateway bind=loopback resolved to non-loopback host %s; refusing fallback to a network bind",
			decision.bindHost,
		)
	}
	if decision.bindMode != BindCustom {
		return nil
	}
	if decision.customBindHost == "" {
		return fmt.Errorf("gateway.bind=custom requires gateway.customBindHost")
	}
	if !isValidIPv4(decision.customBindHost) {
		return fmt.Errorf("gateway.bind=custom requires a valid IPv4 customBindHost (got %s)", decision.customBindHost)
	}
	if decision.bindHost != decision.customBindHost {
		return fmt.Errorf(
			"gateway bind=custom requested %s but resolved %s; refusing fallback",
			decision.customBindHost,
			decision.bindHost,
		)
	}
	return nil
}

func validateGatewayTailscaleDecision(decision gatewayRuntimeDecision) error {
	if decision.tailscaleMode == TailscaleFunnel && decision.authMode != AuthModePassword {
		return fmt.Errorf(
			"tailscale funnel requires gateway auth mode=password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)",
		)
	}
	if decision.tailscaleMode != TailscaleOff && !isLoopbackHost(decision.bindHost) {
		return fmt.Errorf("tailscale serve/funnel requires gateway bind=loopback (127.0.0.1)")
	}
	return nil
}

func validateGatewayNetworkExposure(decision gatewayRuntimeDecision) error {
	loopback := isLoopbackHost(decision.bindHost)
	if !loopback && !decision.resolvedAuth.HasSharedSecret() && decision.authMode != AuthModeTrustedProxy {
		return fmt.Errorf(
			"refusing to bind gateway to %s:%d without auth (set gateway.auth.token/password, or set DENEB_GATEWAY_TOKEN/DENEB_GATEWAY_PASSWORD)",
			decision.bindHost,
			decision.port,
		)
	}
	if decision.controlUIEnabled && !loopback && len(decision.controlUIAllowedOrigins) == 0 && !decision.dangerouslyAllowHostHeader {
		return fmt.Errorf(
			"non-loopback Control UI requires gateway.controlUi.allowedOrigins (set explicit origins), " +
				"or set gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback=true",
		)
	}
	return nil
}

func validateGatewayTrustedProxyDecision(decision gatewayRuntimeDecision) error {
	if decision.authMode != AuthModeTrustedProxy {
		return nil
	}
	if len(decision.trustedProxies) == 0 {
		return fmt.Errorf(
			"gateway auth mode=trusted-proxy requires gateway.trustedProxies to be configured with at least one proxy IP",
		)
	}
	if !isLoopbackHost(decision.bindHost) {
		return nil
	}
	hasLoopback := isTrustedProxyAddress("127.0.0.1", decision.trustedProxies) ||
		isTrustedProxyAddress("::1", decision.trustedProxies)
	if !hasLoopback {
		return fmt.Errorf(
			"gateway auth mode=trusted-proxy with bind=loopback requires gateway.trustedProxies to include 127.0.0.1, ::1, or a loopback CIDR",
		)
	}
	return nil
}

func (decision gatewayRuntimeDecision) runtimeConfig() *GatewayRuntimeConfig {
	return &GatewayRuntimeConfig{
		BindHost:          decision.bindHost,
		Port:              decision.port,
		ControlUIEnabled:  decision.controlUIEnabled,
		ControlUIBasePath: decision.controlUIBasePath,
		ControlUIRoot:     decision.controlUIRoot,
		ResolvedAuth:      decision.resolvedAuth,
		AuthMode:          decision.authMode,
		TailscaleConfig:   decision.tailscaleConfig,
		TailscaleMode:     decision.tailscaleMode,
		TrustedProxies:    decision.trustedProxies,
		ReloadConfig:      decision.reloadConfig,
	}
}

// resolveBindHost maps a bind mode to an IP address.
func resolveBindHost(mode, customHost string, logger *slog.Logger) (string, error) {
	switch NormalizeBindMode(mode) {
	case BindLoopback, "":
		return "127.0.0.1", nil
	case BindLAN:
		return "0.0.0.0", nil
	case BindAuto:
		// Prefer loopback; this simplified version always returns loopback.
		// Full implementation would check if loopback is available.
		return "127.0.0.1", nil
	case BindTailnet:
		// Try to find a Tailscale IP (100.64.0.0/10).
		if ip := findTailscaleIP(logger); ip != "" {
			return ip, nil
		}
		return "127.0.0.1", nil
	case BindCustom:
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
func findTailscaleIP(logger *slog.Logger) string {
	_, tsNet, _ := net.ParseCIDR("100.64.0.0/10")
	ifaces, err := net.Interfaces()
	if err != nil {
		logger.Warn("failed to enumerate network interfaces for Tailscale IP detection", "error", err)
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
	logger.Warn("no Tailscale IP found in 100.64.0.0/10 range; falling back to loopback")
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
