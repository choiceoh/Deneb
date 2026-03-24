package auth

import "testing"

func TestCheckBrowserOrigin_Allowlist(t *testing.T) {
	result := CheckBrowserOrigin("", "https://example.com", []string{"https://example.com"}, false, false)
	if !result.OK || result.MatchedBy != "allowlist" {
		t.Errorf("expected allowlist match, got %+v", result)
	}
}

func TestCheckBrowserOrigin_Wildcard(t *testing.T) {
	result := CheckBrowserOrigin("", "https://anything.com", []string{"*"}, false, false)
	if !result.OK || result.MatchedBy != "allowlist" {
		t.Errorf("expected wildcard match, got %+v", result)
	}
}

func TestCheckBrowserOrigin_MissingOrigin(t *testing.T) {
	result := CheckBrowserOrigin("", "", nil, false, false)
	if result.OK {
		t.Error("expected rejection for missing origin")
	}
}

func TestCheckBrowserOrigin_NullOrigin(t *testing.T) {
	result := CheckBrowserOrigin("", "null", nil, false, false)
	if result.OK {
		t.Error("expected rejection for null origin")
	}
}

func TestCheckBrowserOrigin_HostHeaderFallback(t *testing.T) {
	result := CheckBrowserOrigin("example.com", "https://example.com", nil, true, false)
	if !result.OK || result.MatchedBy != "host-header-fallback" {
		t.Errorf("expected host-header-fallback, got %+v", result)
	}
}

func TestCheckBrowserOrigin_HostHeaderFallbackDisabled(t *testing.T) {
	result := CheckBrowserOrigin("example.com", "https://example.com", nil, false, false)
	if result.OK {
		t.Error("expected rejection when host-header-fallback is disabled")
	}
}

func TestCheckBrowserOrigin_LocalLoopback(t *testing.T) {
	result := CheckBrowserOrigin("", "http://localhost:3000", nil, false, true)
	if !result.OK || result.MatchedBy != "local-loopback" {
		t.Errorf("expected local-loopback, got %+v", result)
	}
}

func TestCheckBrowserOrigin_LocalLoopbackNotLocal(t *testing.T) {
	result := CheckBrowserOrigin("", "http://localhost:3000", nil, false, false)
	if result.OK {
		t.Error("expected rejection for non-local client")
	}
}

func TestCheckBrowserOrigin_127001(t *testing.T) {
	result := CheckBrowserOrigin("", "http://127.0.0.1:8080", nil, false, true)
	if !result.OK || result.MatchedBy != "local-loopback" {
		t.Errorf("expected local-loopback for 127.0.0.1, got %+v", result)
	}
}

func TestCheckBrowserOrigin_NotAllowed(t *testing.T) {
	result := CheckBrowserOrigin("other.com", "https://evil.com", []string{"https://good.com"}, true, false)
	if result.OK {
		t.Error("expected rejection for non-allowed origin")
	}
	if result.Reason != "origin not allowed" {
		t.Errorf("expected 'origin not allowed', got %q", result.Reason)
	}
}

func TestCheckBrowserOrigin_CaseInsensitive(t *testing.T) {
	result := CheckBrowserOrigin("", "HTTPS://EXAMPLE.COM", []string{"https://example.com"}, false, false)
	if !result.OK {
		t.Errorf("expected case-insensitive match, got %+v", result)
	}
}
