package config

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestResolveGatewayRuntimeConfigDefaults(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)

	auth := ResolvedGatewayAuth{Mode: "token", Token: "test-token"}
	rtCfg, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Auth:   &auth,
	})
	testutil.NoError(t, err)
	if rtCfg.BindHost != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1, got %q", rtCfg.BindHost)
	}
	if rtCfg.Port != 18789 {
		t.Errorf("expected port 18789, got %d", rtCfg.Port)
	}
	if !rtCfg.ControlUIEnabled {
		t.Error("control UI should be enabled by default")
	}
	if rtCfg.ControlUIBasePath != "/" {
		t.Errorf("expected basePath=/, got %q", rtCfg.ControlUIBasePath)
	}
	if rtCfg.AuthMode != "token" {
		t.Errorf("expected auth mode=token, got %q", rtCfg.AuthMode)
	}
	if rtCfg.TailscaleMode != "off" {
		t.Errorf("expected tailscale mode=off, got %q", rtCfg.TailscaleMode)
	}
	if rtCfg.ChannelHealthCheckMinutes != 5 {
		t.Errorf("expected health check=5, got %d", rtCfg.ChannelHealthCheckMinutes)
	}
}

func TestResolveGatewayRuntimeConfigBindOverride(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)
	// Non-loopback Control UI requires allowedOrigins or the dangerous fallback flag.
	dangerousFlag := true
	cfg.Gateway.ControlUI.DangerouslyAllowHostHeaderOriginFallback = &dangerousFlag

	auth := ResolvedGatewayAuth{Mode: "token", Token: "test-token"}
	rtCfg, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Bind:   "lan",
		Auth:   &auth,
	})
	testutil.NoError(t, err)
	if rtCfg.BindHost != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0 for lan bind, got %q", rtCfg.BindHost)
	}
}

func TestResolveGatewayRuntimeConfigNonLoopbackNoAuth(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)

	auth := ResolvedGatewayAuth{Mode: "token"} // No token.
	_, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Bind:   "lan",
		Auth:   &auth,
	})
	if err == nil {
		t.Error("expected error for non-loopback without auth")
	}
}

func TestResolveGatewayRuntimeConfigFunnelRequiresPassword(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)

	auth := ResolvedGatewayAuth{Mode: "token", Token: "test-token"}
	_, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config:            &cfg,
		Port:              18789,
		Auth:              &auth,
		TailscaleOverride: &GatewayTailscaleConfig{Mode: "funnel"},
	})
	if err == nil {
		t.Error("expected error for funnel without password auth")
	}
}

func TestResolveGatewayRuntimeConfigTrustedProxyRequiresProxies(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)

	auth := ResolvedGatewayAuth{Mode: "trusted-proxy"}
	_, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Auth:   &auth,
	})
	if err == nil {
		t.Error("expected error for trusted-proxy without trustedProxies")
	}
}

func TestResolveGatewayRuntimeConfigTrustedProxyLoopback(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)
	cfg.Gateway.TrustedProxies = []string{"127.0.0.1"}

	auth := ResolvedGatewayAuth{
		Mode:         "trusted-proxy",
		TrustedProxy: &GatewayTrustedProxyConfig{UserHeader: "x-remote-user"},
	}
	rtCfg, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Auth:   &auth,
	})
	testutil.NoError(t, err)
	if rtCfg.AuthMode != "trusted-proxy" {
		t.Errorf("expected trusted-proxy, got %q", rtCfg.AuthMode)
	}
}

func TestResolveGatewayRuntimeConfigTrustedProxyLoopbackMissing(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)
	cfg.Gateway.TrustedProxies = []string{"10.0.0.1"} // Not loopback.

	auth := ResolvedGatewayAuth{
		Mode:         "trusted-proxy",
		TrustedProxy: &GatewayTrustedProxyConfig{UserHeader: "x-remote-user"},
	}
	_, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Auth:   &auth,
	})
	if err == nil {
		t.Error("expected error for trusted-proxy on loopback without loopback proxy")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost", true},
		{"0.0.0.0", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
	}
	for _, tt := range tests {
		if got := isLoopbackHost(tt.host); got != tt.expected {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.expected)
		}
	}
}

func TestIsValidIPv4(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"127.0.0.1", true},
		{"0.0.0.0", true},
		{"192.168.1.1", true},
		{"::1", false},
		{"not-an-ip", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isValidIPv4(tt.input); got != tt.expected {
			t.Errorf("isValidIPv4(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsTrustedProxyAddress(t *testing.T) {
	proxies := []string{"10.0.0.1", "192.168.0.0/24", "127.0.0.1"}

	if !isTrustedProxyAddress("10.0.0.1", proxies) {
		t.Error("exact match should be trusted")
	}
	if !isTrustedProxyAddress("192.168.0.55", proxies) {
		t.Error("CIDR match should be trusted")
	}
	if isTrustedProxyAddress("10.0.0.2", proxies) {
		t.Error("non-matching IP should not be trusted")
	}
	if !isTrustedProxyAddress("127.0.0.1", proxies) {
		t.Error("loopback in list should be trusted")
	}
}

func TestNormalizeControlUIBasePath(t *testing.T) {
	tests := []struct {
		input    *GatewayControlUIConfig
		expected string
	}{
		{nil, "/"},
		{&GatewayControlUIConfig{}, "/"},
		{&GatewayControlUIConfig{BasePath: ""}, "/"},
		{&GatewayControlUIConfig{BasePath: "/"}, "/"},
		{&GatewayControlUIConfig{BasePath: "/deneb"}, "/deneb"},
		{&GatewayControlUIConfig{BasePath: "/deneb/"}, "/deneb"},
		{&GatewayControlUIConfig{BasePath: "deneb"}, "/deneb"},
	}
	for _, tt := range tests {
		got := normalizeControlUIBasePath(tt.input)
		if got != tt.expected {
			bp := ""
			if tt.input != nil {
				bp = tt.input.BasePath
			}
			t.Errorf("normalizeControlUIBasePath(%q) = %q, want %q", bp, got, tt.expected)
		}
	}
}

func TestControlUIDisabled(t *testing.T) {
	cfg := DenebConfig{}
	applyDefaults(&cfg)
	disabled := false
	cfg.Gateway.ControlUI.Enabled = &disabled

	auth := ResolvedGatewayAuth{Mode: "token", Token: "test"}
	rtCfg, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
		Auth:   &auth,
	})
	testutil.NoError(t, err)
	if rtCfg.ControlUIEnabled {
		t.Error("control UI should be disabled")
	}
}

func TestResolvedGatewayAuthHasSharedSecret(t *testing.T) {
	tests := []struct {
		auth     ResolvedGatewayAuth
		expected bool
	}{
		{ResolvedGatewayAuth{Mode: "none"}, false},
		{ResolvedGatewayAuth{Mode: "token", Token: ""}, false},
		{ResolvedGatewayAuth{Mode: "token", Token: "abc"}, true},
		{ResolvedGatewayAuth{Mode: "password", Password: ""}, false},
		{ResolvedGatewayAuth{Mode: "password", Password: "abc"}, true},
		{ResolvedGatewayAuth{Mode: "trusted-proxy"}, false},
	}
	for _, tt := range tests {
		if got := tt.auth.HasSharedSecret(); got != tt.expected {
			t.Errorf("HasSharedSecret(%v) = %v, want %v", tt.auth.Mode, got, tt.expected)
		}
	}
}
