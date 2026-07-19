package appupdate

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Handler serves authenticated app update metadata and APK downloads.
type Handler struct {
	logger *slog.Logger
}

// New creates an app-update HTTP handler.
func New(logger *slog.Logger) *Handler { return &Handler{logger: logger} }

// In-app update endpoints, served from the gateway's own port so the single
// cloudflare ingress (deneb.topworks.ltd -> :18789) carries updates too. The
// previous design served version.json + the APK from a separate :19010
// http.server that the tunnel never routed, so deneb.topworks.ltd clients hung
// forever on the update check. The native client now hits these two routes on
// the same base URL it already uses for chat:
//
//	GET /api/v1/app/update/manifest                          (X-Deneb-Client-Token header)
//	GET /api/v1/app/update/download?file=<name>&clientToken=<tok>

// denebApkDir is the directory holding published APKs (plus an optional
// version.json for release notes). Overridable for non-standard deployments.
func denebApkDir() string {
	if d := strings.TrimSpace(os.Getenv("DENEB_APK_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", "deneb-apk")
	}
	return filepath.Join(home, ".cache", "deneb-apk")
}

// apkFilePattern matches a published flavor APK by its versionCode. The current
// filename shape is deneb-<code>-<sha>-<variant>.apk (e.g.
// deneb-187-a1b2c3d4-fossRelease.apk). It also accepts the legacy
// deneb-<name>-<code>-... shape still lingering in the serve dir from before the
// semantic versionName was dropped, so the scan keeps finding those during the
// transition. The optional leading group only matches a dotted version (e.g.
// 2.9.60-); the code is the first dot-less integer segment.
var apkFilePattern = regexp.MustCompile(`^deneb-(?:\d+(?:\.\d+)+-)?(\d+)-.+\.apk$`)

type appUpdateManifest struct {
	Code  int    `json:"code"`
	File  string `json:"file"`
	Notes string `json:"notes"`
	// DownloadToken is a short-lived HMAC token bound to File — the download
	// URL carries it instead of the long-lived client token (URLs leak via
	// access logs/history in ways headers do not). Old clients ignore the
	// field and keep using the legacy clientToken query.
	DownloadToken string `json:"downloadToken,omitempty"`
}

// apkDownloadTokenPurpose namespaces update-download tokens; apkDownloadTokenTTL
// covers the gap between an update check and the user tapping download.
const (
	apkDownloadTokenPurpose = "apk-download"
	apkDownloadTokenTTL     = 15 * time.Minute
)

var (
	downloadTokenKeyOnce sync.Once
	downloadTokenKey     []byte
)

func downloadTokenSigningKey() []byte {
	downloadTokenKeyOnce.Do(func() {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			// Leave the key empty so minting fails closed. A gateway restart
			// intentionally invalidates outstanding download tokens.
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

// mintDownloadToken issues a short-lived token authorizing one named file.
func mintDownloadToken(purpose, name string, ttl time.Duration) string {
	if len(downloadTokenSigningKey()) == 0 || name == "" {
		return ""
	}
	exp := time.Now().Add(ttl).Unix()
	return strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(downloadTokenMAC(purpose, name, exp))
}

// verifyDownloadToken checks signature, expiry, purpose, and file binding.
func verifyDownloadToken(purpose, name, token string) bool {
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

// latestPublishedApk scans the APK dir for the highest version code. The files
// on disk are the source of truth — a stale version.json can't mask a newer
// build — and version.json contributes only the human-readable notes.
func latestPublishedApk(dir string) (appUpdateManifest, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return appUpdateManifest{}, false
	}
	best := appUpdateManifest{Code: -1}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		groups := apkFilePattern.FindStringSubmatch(e.Name())
		if groups == nil {
			continue
		}
		code, err := strconv.Atoi(groups[1])
		if err != nil {
			continue
		}
		if code > best.Code {
			best = appUpdateManifest{Code: code, File: e.Name()}
		}
	}
	if best.Code < 0 {
		return appUpdateManifest{}, false
	}
	best.Notes = apkReleaseNotesForCode(dir, best.Code)
	return best, true
}

// apkReleaseNotesForCode pulls the optional "notes" string from version.json,
// but ONLY when version.json describes the same build as the latest APK on disk
// (its "code" matches). version.json is rewritten on every publish and holds just
// one build's notes, so it can lag the newest APK — a concurrent publish that
// finished later, or a notes-less hotfix, leaves version.json pointing at an
// older build than the highest-coded APK the disk scan selects. Returning its
// notes unconditionally then captions the new build with an OLDER build's
// changelog (the "예전 패치노트가 다시 올라온다" bug). On a mismatch (or missing /
// malformed file) we return empty: no notes is better than stale notes.
func apkReleaseNotesForCode(dir string, code int) string {
	raw, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		return ""
	}
	var vj struct {
		Code  int    `json:"code"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &vj); err != nil {
		return ""
	}
	if vj.Code != code {
		return ""
	}
	return strings.TrimSpace(vj.Notes)
}

// handleAppUpdateManifest returns metadata for the latest published APK. The
// native client compares the returned code to its compiled-in version and
// offers a one-tap download when newer.
func (s *Handler) Manifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.Authenticate(w, r, s.logger); !ok {
		return
	}
	m, ok := latestPublishedApk(denebApkDir())
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "no published apk"})
		return
	}
	m.DownloadToken = mintDownloadToken(apkDownloadTokenPurpose, m.File, apkDownloadTokenTTL)
	s.writeJSON(w, http.StatusOK, m)
}

// handleAppUpdateDownload streams a published APK. The client token rides in the
// query string because the browser opening the download link cannot set the
// X-Deneb-Client-Token header (same shape as the Gmail attachment route).
func (s *Handler) Download(w http.ResponseWriter, r *http.Request) {
	// filepath.Base strips any directory segments and the .apk suffix check
	// keeps this confined to APK files inside the serve dir — no traversal.
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("file")))
	if name == "." || name == string(filepath.Separator) || !strings.HasSuffix(name, ".apk") {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid file"})
		return
	}
	// Preferred auth: the short-lived token the manifest minted for THIS file.
	// When the dl param is present its verdict is final (no long-lived-token
	// fallback for a bad short token); absent → legacy clientToken query for
	// clients from before the manifest carried downloadToken.
	if dl := strings.TrimSpace(r.URL.Query().Get("dl")); dl != "" {
		if !verifyDownloadToken(apkDownloadTokenPurpose, name, dl) {
			s.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or expired download token"})
			return
		}
	} else if _, ok := nativeauth.AuthenticateDownload(w, r, s.logger); !ok {
		return
	}
	// A multi-MB APK over a slow mobile link can outlast the global WriteTimeout.
	disableWriteDeadline(w)
	full := filepath.Join(denebApkDir(), name)
	f, err := os.Open(full) //nolint:gosec // name is filepath.Base'd and .apk-suffixed; confined to the serve dir
	if err != nil {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "apk not found"})
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "apk not found"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Server", "deneb-gateway")
	// ServeContent gives us Content-Length + Range support (resumable download).
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && s.logger != nil {
		httputil.LogEncodeError(s.logger, "app update: json encode error", err)
	}
}

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
