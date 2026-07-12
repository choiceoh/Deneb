package server

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The short-lived download token must bind to one file, expire, and reject
// tampering — it replaces the long-lived client token in URLs.
func TestDownloadToken(t *testing.T) {
	tok := mintDownloadToken("apk-download", "deneb-1.apk", time.Minute)
	if tok == "" {
		t.Fatal("mint returned empty token")
	}
	if !verifyDownloadToken("apk-download", "deneb-1.apk", tok) {
		t.Fatal("fresh token rejected")
	}
	if verifyDownloadToken("apk-download", "deneb-2.apk", tok) {
		t.Fatal("token accepted for a different file")
	}
	if verifyDownloadToken("gmail-attachment", "deneb-1.apk", tok) {
		t.Fatal("token accepted for a different purpose")
	}

	expired := mintDownloadToken("apk-download", "deneb-1.apk", -time.Second)
	if verifyDownloadToken("apk-download", "deneb-1.apk", expired) {
		t.Fatal("expired token accepted")
	}

	// Tamper with the signature and the expiry.
	parts := strings.SplitN(tok, ".", 2)
	if verifyDownloadToken("apk-download", "deneb-1.apk", parts[0]+".deadbeef") {
		t.Fatal("tampered MAC accepted")
	}
	if verifyDownloadToken("apk-download", "deneb-1.apk", "9999999999."+parts[1]) {
		t.Fatal("tampered expiry accepted")
	}
	for _, junk := range []string{"", ".", "abc", "123", "123."} {
		if verifyDownloadToken("apk-download", "deneb-1.apk", junk) {
			t.Fatalf("junk token %q accepted", junk)
		}
	}
}

// verifyDownloadToken must fail closed when the per-process signing key never
// initialized (crypto/rand failure): an empty HMAC key would otherwise let an
// attacker forge a valid MAC (#3455/#3456).
func TestVerifyDownloadToken_EmptyKeyFailsClosed(t *testing.T) {
	_ = mintDownloadToken("x", "f.apk", time.Minute) // ensure the Once has run
	saved := downloadTokenKey
	downloadTokenKey = []byte{}
	defer func() { downloadTokenKey = saved }()

	// A MAC forged against the empty key must be rejected.
	exp := time.Now().Add(time.Minute).Unix()
	forged := strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(downloadTokenMAC("apk-download", "deneb-1.apk", exp))
	if verifyDownloadToken("apk-download", "deneb-1.apk", forged) {
		t.Fatal("verify accepted a token under an empty signing key")
	}
}
