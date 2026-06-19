package fileshare

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func TestSign_RoundTrip(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	if _, err := clientauth.Generate(); err != nil {
		t.Fatalf("generate client token: %v", err)
	}
	const p = "/메일/견적서.pdf"
	exp := time.Now().Add(time.Hour).Unix()
	sig := Sign(p, exp)

	if !Verify(p, exp, sig) {
		t.Error("valid signature was rejected")
	}
	if Verify("/메일/다른파일.pdf", exp, sig) {
		t.Error("signature accepted for a different path (forgeable!)")
	}
	if Verify(p, exp, sig+"00") {
		t.Error("tampered signature accepted")
	}
	past := time.Now().Add(-time.Hour).Unix()
	if Verify(p, past, Sign(p, past)) {
		t.Error("expired signature accepted")
	}
}

func TestVerify_NoToken(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir()) // no token generated → no signing key
	exp := time.Now().Add(time.Hour).Unix()
	if Verify("/x.pdf", exp, Sign("/x.pdf", exp)) {
		t.Error("Verify must be false when no client token is configured")
	}
}

func TestLink(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_PUBLIC_BASE_URL", "https://deneb.example.com")

	// No token yet → no signing key → empty link (caller must fall back).
	if got := Link("/x.pdf"); got != "" {
		t.Errorf("Link without token = %q, want empty", got)
	}
	if _, err := clientauth.Generate(); err != nil {
		t.Fatalf("generate client token: %v", err)
	}
	link := Link("/메일/견적서.pdf")
	if !strings.HasPrefix(link, "https://deneb.example.com/api/v1/files/download?") {
		t.Fatalf("link = %q, want gateway download URL", link)
	}
	if !strings.Contains(link, "sig=") || !strings.Contains(link, "exp=") {
		t.Errorf("link missing sig/exp: %q", link)
	}
}
