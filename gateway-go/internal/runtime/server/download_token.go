package server

// Short-lived signed download tokens.
//
// Browser-initiated downloads (APK updates) cannot set the
// X-Deneb-Client-Token header, so the token has to ride in the URL — but a
// URL leaks in ways a header does not (server/proxy access logs, browser
// history, referrers). Instead of the LONG-LIVED client token, the
// header-authenticated manifest response mints a short-lived HMAC token bound
// to one file name; the download URL carries that. The signing key is
// per-process random: a gateway restart invalidates outstanding tokens, and
// the client simply re-fetches the manifest (its next update check) — no key
// storage, no config surface.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	downloadTokenKeyOnce sync.Once
	downloadTokenKey     []byte
)

func downloadTokenSigningKey() []byte {
	downloadTokenKeyOnce.Do(func() {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			// Leave the key empty: minting fails closed (verify of any token
			// against an empty key still requires a matching MAC, and mint
			// returns "" so no URL ever carries an unverifiable token).
			return
		}
		downloadTokenKey = key
	})
	return downloadTokenKey
}

func downloadTokenMAC(purpose, name string, exp int64) []byte {
	mac := hmac.New(sha256.New, downloadTokenSigningKey())
	fmt.Fprintf(mac, "%s|%s|%d", purpose, name, exp)
	return mac.Sum(nil)
}

// mintDownloadToken issues "<expUnix>.<hexmac>" authorizing one named file for
// ttl. Empty on failure (no signing key).
func mintDownloadToken(purpose, name string, ttl time.Duration) string {
	if len(downloadTokenSigningKey()) == 0 || name == "" {
		return ""
	}
	exp := time.Now().Add(ttl).Unix()
	return strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(downloadTokenMAC(purpose, name, exp))
}

// verifyDownloadToken checks signature, expiry, and file binding.
func verifyDownloadToken(purpose, name, token string) bool {
	// Fail closed if the per-process signing key never initialized (crypto/rand
	// failure): with an empty HMAC key an attacker could forge a valid MAC.
	// mintDownloadToken already refuses to issue in this state; verify must too
	// (reviewer feedback, #3455/#3456).
	if len(downloadTokenSigningKey()) == 0 {
		return false
	}
	expStr, macHex, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || name == "" {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	got, err := hex.DecodeString(macHex)
	if err != nil {
		return false
	}
	return hmac.Equal(got, downloadTokenMAC(purpose, name, exp))
}
