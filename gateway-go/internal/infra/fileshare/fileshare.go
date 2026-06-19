// Package fileshare mints and verifies time-limited, path-scoped download links
// for the local file store — the replacement for Dropbox shared links.
//
// A link is HMAC-SHA256(clientToken, "<vpath>\n<expiryUnix>"). Reusing the
// client token as the signing key means whoever already holds it (the operator)
// can mint links and nobody else can forge them — no separate secret to manage.
// Token rotation invalidates outstanding links, which is acceptable for shares.
//
// Both sides share this one source of truth: the gateway download route calls
// Verify, and the chat tool's "share" action calls Link, so the signature
// scheme lives in exactly one place.
package fileshare

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// TTL bounds how long a freshly minted share link stays valid.
const TTL = 7 * 24 * time.Hour

// DownloadPath is the gateway route that serves signed (and client-token) links.
const DownloadPath = "/api/v1/files/download"

func key() []byte { return []byte(clientauth.Load()) }

// Sign returns the hex HMAC for vpath at expiry exp (unix seconds).
func Sign(vpath string, exp int64) string {
	mac := hmac.New(sha256.New, key())
	mac.Write([]byte(vpath + "\n" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether sig is a valid, unexpired signature for vpath. It
// returns false when no client token is configured (no key), so the route
// uniformly treats false as "not authorized via share link".
func Verify(vpath string, exp int64, sig string) bool {
	if len(key()) == 0 || exp <= 0 || time.Now().Unix() > exp {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(Sign(vpath, exp)), []byte(sig)) == 1
}

// Link builds a TTL-bounded, path-scoped download URL for vpath rooted at the
// gateway public base URL. Returns "" when no public base URL is configured or
// no client token exists (no signing key) — callers must fall back (e.g. tell
// the user to open the file in the native app).
func Link(vpath string) string {
	base := PublicBaseURL()
	if base == "" || len(key()) == 0 {
		return ""
	}
	exp := time.Now().Add(TTL).Unix()
	q := url.Values{}
	q.Set("path", vpath)
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", Sign(vpath, exp))
	return base + DownloadPath + "?" + q.Encode()
}

// PublicBaseURL returns the gateway's externally reachable base URL (no trailing
// slash) from DENEB_PUBLIC_BASE_URL, falling back to the OTA base (DENEB_APK_BASE_URL).
// Empty when neither is set.
func PublicBaseURL() string {
	for _, env := range []string{"DENEB_PUBLIC_BASE_URL", "DENEB_APK_BASE_URL"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	return ""
}
