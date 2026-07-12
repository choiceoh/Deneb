package config

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestResolveGatewayRuntimeConfigDefaultsWithoutLoader(t *testing.T) {
	cfg := DenebConfig{}

	got, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config: &cfg,
		Port:   18789,
	})
	testutil.NoError(t, err)

	want := &GatewayRuntimeConfig{
		BindHost:          "127.0.0.1",
		Port:              18789,
		ControlUIEnabled:  true,
		ControlUIBasePath: "/",
		ResolvedAuth:      ResolvedGatewayAuth{Mode: AuthModeToken},
		AuthMode:          AuthModeToken,
		TailscaleConfig:   GatewayTailscaleConfig{Mode: TailscaleOff},
		TailscaleMode:     TailscaleOff,
		ReloadConfig:      GatewayReloadConfig{Mode: ReloadHybrid},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveGatewayRuntimeConfig() = %#v, want %#v", got, want)
	}
}

func TestResolveGatewayRuntimeConfigSourcePrecedence(t *testing.T) {
	configUIEnabled := false
	overrideUIEnabled := true
	configResetOnExit := false
	overrideResetOnExit := true
	reloadDebounce := 750
	cfg := DenebConfig{Gateway: &GatewayConfig{
		Bind:           BindLAN,
		CustomBindHost: "10.0.0.10",
		ControlUI: &GatewayControlUIConfig{
			Enabled:  &configUIEnabled,
			BasePath: " dashboard/ ",
			Root:     " /srv/deneb-ui ",
		},
		Tailscale: &GatewayTailscaleConfig{
			Mode:        TailscaleOff,
			ResetOnExit: &configResetOnExit,
		},
		TrustedProxies: []string{"10.0.0.1"},
		Reload: &GatewayReloadConfig{
			Mode:       ReloadHot,
			DebounceMs: &reloadDebounce,
		},
	}}
	auth := ResolvedGatewayAuth{Mode: AuthModePassword, Password: "test-password"}

	got, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
		Config:           &cfg,
		Port:             19000,
		Bind:             BindLoopback,
		Host:             "127.0.0.2",
		ControlUIEnabled: &overrideUIEnabled,
		Auth:             &auth,
		TailscaleOverride: &GatewayTailscaleConfig{
			Mode:        TailscaleFunnel,
			ResetOnExit: &overrideResetOnExit,
		},
	})
	testutil.NoError(t, err)

	want := &GatewayRuntimeConfig{
		BindHost:          "127.0.0.2",
		Port:              19000,
		ControlUIEnabled:  true,
		ControlUIBasePath: "/dashboard",
		ControlUIRoot:     "/srv/deneb-ui",
		ResolvedAuth:      auth,
		AuthMode:          AuthModePassword,
		TailscaleConfig: GatewayTailscaleConfig{
			Mode:        TailscaleFunnel,
			ResetOnExit: &overrideResetOnExit,
		},
		TailscaleMode:  TailscaleFunnel,
		TrustedProxies: []string{"10.0.0.1"},
		ReloadConfig: GatewayReloadConfig{
			Mode:       ReloadHot,
			DebounceMs: &reloadDebounce,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveGatewayRuntimeConfig() = %#v, want %#v", got, want)
	}
}

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
		t.Errorf("got %q, want 127.0.0.1", rtCfg.BindHost)
	}
	if rtCfg.Port != 18789 {
		t.Errorf("got %d, want port 18789", rtCfg.Port)
	}
	if !rtCfg.ControlUIEnabled {
		t.Error("control UI should be enabled by default")
	}
	if rtCfg.ControlUIBasePath != "/" {
		t.Errorf("got %q, want basePath=/", rtCfg.ControlUIBasePath)
	}
	if rtCfg.AuthMode != "token" {
		t.Errorf("got %q, want auth mode=token", rtCfg.AuthMode)
	}
	if rtCfg.TailscaleMode != "off" {
		t.Errorf("got %q, want tailscale mode=off", rtCfg.TailscaleMode)
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
		t.Errorf("got %q, want 0.0.0.0 for lan bind", rtCfg.BindHost)
	}
}

// IP-form bind values must resolve identically to their canonical mode names.
func TestResolveGatewayRuntimeConfigBindIPAliases(t *testing.T) {
	dangerousFlag := true
	cases := []struct {
		bind     string
		wantHost string
	}{
		{"0.0.0.0", "0.0.0.0"},
		{"all", "0.0.0.0"},
		{"127.0.0.1", "127.0.0.1"},
		{"localhost", "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.bind, func(t *testing.T) {
			cfg := DenebConfig{}
			applyDefaults(&cfg)
			cfg.Gateway.ControlUI.DangerouslyAllowHostHeaderOriginFallback = &dangerousFlag

			auth := ResolvedGatewayAuth{Mode: "token", Token: "test-token"}
			rtCfg, err := ResolveGatewayRuntimeConfig(RuntimeConfigParams{
				Config: &cfg,
				Port:   18789,
				Bind:   tc.bind,
				Auth:   &auth,
			})
			testutil.NoError(t, err)
			if rtCfg.BindHost != tc.wantHost {
				t.Errorf("bind=%q: got host %q, want %q", tc.bind, rtCfg.BindHost, tc.wantHost)
			}
		})
	}
}

func TestResolveGatewayRuntimeConfigValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		params    RuntimeConfigParams
		wantError string
	}{
		{
			name: "loopback mode rejects a non-loopback host override",
			params: RuntimeConfigParams{
				Config: &DenebConfig{}, Port: 18789, Bind: BindLoopback, Host: "0.0.0.0",
				Auth: &ResolvedGatewayAuth{Mode: AuthModeToken, Token: "test-token"},
			},
			wantError: "gateway bind=loopback resolved to non-loopback host 0.0.0.0; refusing fallback to a network bind",
		},
		{
			name: "custom mode requires a configured host",
			params: RuntimeConfigParams{
				Config: &DenebConfig{Gateway: &GatewayConfig{Bind: BindCustom}}, Port: 18789,
			},
			wantError: "gateway.bind=custom requires gateway.customBindHost",
		},
		{
			name: "custom mode requires IPv4",
			params: RuntimeConfigParams{
				Config: &DenebConfig{Gateway: &GatewayConfig{Bind: BindCustom, CustomBindHost: "not-an-ip"}}, Port: 18789,
			},
			wantError: "gateway.bind=custom requires a valid IPv4 customBindHost (got not-an-ip)",
		},
		{
			name: "custom mode rejects a host fallback before auth validation",
			params: RuntimeConfigParams{
				Config: &DenebConfig{Gateway: &GatewayConfig{Bind: BindCustom, CustomBindHost: "10.0.0.1"}},
				Port:   18789,
				Host:   "10.0.0.2",
				Auth:   &ResolvedGatewayAuth{Mode: AuthModeToken},
			},
			wantError: "gateway bind=custom requested 10.0.0.1 but resolved 10.0.0.2; refusing fallback",
		},
		{
			name: "funnel auth is checked before bind and shared-secret constraints",
			params: RuntimeConfigParams{
				Config:            &DenebConfig{},
				Port:              18789,
				Bind:              BindLAN,
				Auth:              &ResolvedGatewayAuth{Mode: AuthModeToken},
				TailscaleOverride: &GatewayTailscaleConfig{Mode: TailscaleFunnel},
			},
			wantError: "tailscale funnel requires gateway auth mode=password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)",
		},
		{
			name: "tailscale bind is checked before shared-secret constraints",
			params: RuntimeConfigParams{
				Config:            &DenebConfig{},
				Port:              18789,
				Bind:              BindLAN,
				Auth:              &ResolvedGatewayAuth{Mode: AuthModeToken},
				TailscaleOverride: &GatewayTailscaleConfig{Mode: TailscaleServe},
			},
			wantError: "tailscale serve/funnel requires gateway bind=loopback (127.0.0.1)",
		},
		{
			name: "non-loopback auth is checked before control UI origins",
			params: RuntimeConfigParams{
				Config: &DenebConfig{}, Port: 18789, Bind: BindLAN,
				Auth: &ResolvedGatewayAuth{Mode: AuthModeToken},
			},
			wantError: "refusing to bind gateway to 0.0.0.0:18789 without auth (set gateway.auth.token/password, or set DENEB_GATEWAY_TOKEN/DENEB_GATEWAY_PASSWORD)",
		},
		{
			name: "non-loopback control UI requires origins",
			params: RuntimeConfigParams{
				Config: &DenebConfig{}, Port: 18789, Bind: BindLAN,
				Auth: &ResolvedGatewayAuth{Mode: AuthModeToken, Token: "test-token"},
			},
			wantError: "non-loopback Control UI requires gateway.controlUi.allowedOrigins (set explicit origins), or set gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback=true",
		},
		{
			name: "trusted proxy auth requires a proxy list",
			params: RuntimeConfigParams{
				Config: &DenebConfig{}, Port: 18789,
				Auth: &ResolvedGatewayAuth{Mode: AuthModeTrustedProxy},
			},
			wantError: "gateway auth mode=trusted-proxy requires gateway.trustedProxies to be configured with at least one proxy IP",
		},
		{
			name: "trusted proxy loopback bind requires a loopback proxy",
			params: RuntimeConfigParams{
				Config: &DenebConfig{Gateway: &GatewayConfig{TrustedProxies: []string{"10.0.0.1"}}}, Port: 18789,
				Auth: &ResolvedGatewayAuth{Mode: AuthModeTrustedProxy},
			},
			wantError: "gateway auth mode=trusted-proxy with bind=loopback requires gateway.trustedProxies to include 127.0.0.1, ::1, or a loopback CIDR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveGatewayRuntimeConfig(tt.params)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("ResolveGatewayRuntimeConfig() error = %v, want %q", err, tt.wantError)
			}
		})
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
		t.Errorf("got %q, want trusted-proxy", rtCfg.AuthMode)
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
