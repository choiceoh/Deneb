package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

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

// apkFilePattern matches a published flavor APK: deneb-<name>-<code>-<variant>.apk
// (e.g. deneb-2.9.30-153-fossDebug.apk). The variant segment may itself contain
// hyphens, so name is captured non-greedily up to the numeric code.
var apkFilePattern = regexp.MustCompile(`^deneb-(.+?)-(\d+)-.+\.apk$`)

type appUpdateManifest struct {
	Code  int    `json:"code"`
	Name  string `json:"name"`
	File  string `json:"file"`
	Notes string `json:"notes"`
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
		code, err := strconv.Atoi(groups[2])
		if err != nil {
			continue
		}
		if code > best.Code {
			best = appUpdateManifest{Code: code, Name: groups[1], File: e.Name()}
		}
	}
	if best.Code < 0 {
		return appUpdateManifest{}, false
	}
	best.Notes = apkReleaseNotes(dir)
	return best, true
}

// apkReleaseNotes pulls the optional "notes" string from version.json if it
// exists; absence is fine (notes are cosmetic).
func apkReleaseNotes(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		return ""
	}
	var vj struct {
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &vj); err != nil {
		return ""
	}
	return strings.TrimSpace(vj.Notes)
}

// handleAppUpdateManifest returns metadata for the latest published APK. The
// native client compares the returned code to its compiled-in version and
// offers a one-tap download when newer.
func (s *Server) handleAppUpdateManifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappRequest(w, r); !ok {
		return
	}
	m, ok := latestPublishedApk(denebApkDir())
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "no published apk"})
		return
	}
	s.writeJSON(w, http.StatusOK, m)
}

// handleAppUpdateDownload streams a published APK. The client token rides in the
// query string because the browser opening the download link cannot set the
// X-Deneb-Client-Token header (same shape as the Gmail attachment route).
func (s *Server) handleAppUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappDownloadRequest(w, r); !ok {
		return
	}
	// filepath.Base strips any directory segments and the .apk suffix check
	// keeps this confined to APK files inside the serve dir — no traversal.
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("file")))
	if name == "." || name == string(filepath.Separator) || !strings.HasSuffix(name, ".apk") {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid file"})
		return
	}
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
