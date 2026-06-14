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
	// A multi-MB APK over a slow mobile link can outlast the global WriteTimeout.
	disableWriteDeadline(w)
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
