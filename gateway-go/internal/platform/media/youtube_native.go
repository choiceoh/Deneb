// Native YouTube transcript + metadata extraction — no external dependency.
//
// This is the primary path for ExtractYouTubeTranscript. It talks to YouTube's
// internal "innertube" player API directly over HTTP and parses the returned
// caption tracks, so the common "summarize this YouTube link" case works with
// zero external tooling (yt-dlp / ffmpeg / a Python runtime).
//
// Scope is deliberately limited to captions + metadata. Audio/video DOWNLOAD
// (the ASR and watch-frame paths) still requires yt-dlp because it depends on
// deciphering YouTube's signature cipher and `n`-parameter throttling, which is
// a perpetually-moving target best left to yt-dlp. When this native path yields
// no transcript, ExtractYouTubeTranscript falls back to the yt-dlp flow, which
// adds the ASR fallback for caption-less videos.
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// innertubeWebKey is YouTube's public WEB-client innertube API key. It is baked
// into youtube.com's own web client and is not a secret. Overridable via
// DENEB_YT_INNERTUBE_KEY in case a future YouTube change rotates it (the yt-dlp
// fallback covers us if this path stops working entirely).
const innertubeWebKey = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8" //nolint:gosec // G101 false positive: public WEB-client innertube key baked into youtube.com, not a secret/credential

// innertubeClientVersion is the WEB client version sent in the innertube
// context. YouTube is lenient about the exact value; overridable via
// DENEB_YT_INNERTUBE_CLIENT_VERSION.
const innertubeClientVersion = "2.20240726.00.00"

// nativeBrowserUA is sent on innertube/timedtext requests. The shared transport
// would otherwise attach "Deneb-Gateway/..."; a browser UA keeps YouTube from
// treating the call as an unknown client.
const nativeBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// nativePlayerTimeout bounds each native HTTP call (player + caption fetch).
const nativePlayerTimeout = 15 * time.Second

func innertubeKey() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_YT_INNERTUBE_KEY")); v != "" {
		return v
	}
	return innertubeWebKey
}

func innertubeVersion() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_YT_INNERTUBE_CLIENT_VERSION")); v != "" {
		return v
	}
	return innertubeClientVersion
}

// extractVideoID pulls the 11-char video ID out of any supported YouTube URL
// (reusing youtubeURLPattern), or accepts a bare ID. Returns "" when none.
func extractVideoID(input string) string {
	input = strings.TrimSpace(input)
	if m := youtubeURLPattern.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	if len(input) == 11 && isVideoID(input) {
		return input
	}
	return ""
}

func isVideoID(s string) bool {
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// innertubePlayerResponse is the subset of the /youtubei/v1/player JSON we use.
type innertubePlayerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	VideoDetails struct {
		Title            string `json:"title"`
		Author           string `json:"author"`
		LengthSeconds    string `json:"lengthSeconds"`
		ViewCount        string `json:"viewCount"`
		ShortDescription string `json:"shortDescription"`
	} `json:"videoDetails"`
	Microformat struct {
		PlayerMicroformatRenderer struct {
			PublishDate string `json:"publishDate"`
			UploadDate  string `json:"uploadDate"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
	Captions struct {
		PlayerCaptionsTracklistRenderer struct {
			CaptionTracks []captionTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"` // "asr" for auto-generated, empty for manual
}

// extractTranscriptNative attempts metadata + caption extraction via YouTube's
// innertube API. Returns a populated *YouTubeResult on success (Transcript may be
// "" when the video genuinely has no captions), or nil when the native path
// can't be used (bad ID, network/parse error, or the video isn't playable for
// the WEB client) so the caller falls back to yt-dlp.
func extractTranscriptNative(ctx context.Context, videoURL string) *YouTubeResult {
	videoID := extractVideoID(videoURL)
	if videoID == "" {
		return nil
	}

	player, err := fetchInnertubePlayer(ctx, videoID)
	if err != nil {
		slog.Debug("native youtube player fetch failed", "video", videoID, "error", err)
		return nil
	}
	if status := player.PlayabilityStatus.Status; status != "" && status != "OK" {
		// LOGIN_REQUIRED / UNPLAYABLE / ERROR — let yt-dlp (other clients) try.
		slog.Debug("native youtube not playable", "video", videoID, "status", status,
			"reason", player.PlayabilityStatus.Reason)
		return nil
	}

	durSec, _ := strconv.Atoi(strings.TrimSpace(player.VideoDetails.LengthSeconds))
	views, _ := strconv.ParseInt(strings.TrimSpace(player.VideoDetails.ViewCount), 10, 64)

	result := &YouTubeResult{
		Title:       player.VideoDetails.Title,
		Channel:     player.VideoDetails.Author,
		DurationSec: durSec,
		Duration:    formatDuration(durSec),
		UploadDate:  normalizeUploadDate(firstNonEmpty(player.Microformat.PlayerMicroformatRenderer.PublishDate, player.Microformat.PlayerMicroformatRenderer.UploadDate)),
		ViewCount:   views,
		Description: truncateString(player.VideoDetails.ShortDescription, 1000),
		URL:         videoURL,
	}

	tracks := player.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks
	if baseURL, lang := selectCaptionTrack(tracks); baseURL != "" {
		if text, err := fetchTimedText(ctx, baseURL); err == nil && text != "" {
			result.Transcript = text
			result.Language = lang
		} else if err != nil {
			slog.Debug("native youtube caption fetch failed", "video", videoID, "lang", lang, "error", err)
		}
	}

	return result
}

// fetchInnertubePlayer POSTs to the innertube player endpoint and decodes the
// response. The WEB client context is what youtube.com's own page sends.
func fetchInnertubePlayer(ctx context.Context, videoID string) (*innertubePlayerResponse, error) {
	body := map[string]any{
		"videoId": videoID,
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": innertubeVersion(),
				"hl":            "ko",
				"gl":            "KR",
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	endpoint := "https://www.youtube.com/youtubei/v1/player?key=" + innertubeKey() + "&prettyPrint=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", nativeBrowserUA)
	req.Header.Set("Accept-Language", "ko,en;q=0.9")
	req.Header.Set("Origin", "https://www.youtube.com")
	req.Header.Set("X-YouTube-Client-Name", "1")
	req.Header.Set("X-YouTube-Client-Version", innertubeVersion())

	resp, err := httputil.NewClient(nativePlayerTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("innertube player HTTP %d", resp.StatusCode)
	}

	// Player responses are typically a few hundred KB; cap to guard against a
	// pathological body.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var pr innertubePlayerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("parse player response: %w", err)
	}
	return &pr, nil
}

// selectCaptionTrack picks a caption track, mirroring the yt-dlp path's
// preference: ko manual → en manual → ko auto → en auto → any auto. Returns the
// track baseUrl and a human language label ("ko", "en (auto)", "auto", ...).
func selectCaptionTrack(tracks []captionTrack) (baseURL, label string) {
	find := func(lang string, auto bool) (string, string, bool) {
		for _, t := range tracks {
			if t.BaseURL == "" {
				continue
			}
			isAuto := t.Kind == "asr"
			if isAuto != auto {
				continue
			}
			if lang != "" && t.LanguageCode != lang {
				continue
			}
			l := t.LanguageCode
			if l == "" {
				l = "unknown"
			}
			if auto {
				if lang == "" {
					l = "auto"
				} else {
					l += " (auto)"
				}
			}
			return t.BaseURL, l, true
		}
		return "", "", false
	}

	for _, lang := range []string{"ko", "en"} {
		if u, l, ok := find(lang, false); ok {
			return u, l
		}
	}
	for _, lang := range []string{"ko", "en"} {
		if u, l, ok := find(lang, true); ok {
			return u, l
		}
	}
	if u, l, ok := find("", true); ok {
		return u, l
	}
	return "", ""
}

// timedTextDoc parses the default (XML) timedtext caption format returned by a
// caption track baseUrl.
type timedTextDoc struct {
	Texts []struct {
		Content string `xml:",chardata"`
	} `xml:"text"`
}

// fetchTimedText downloads and flattens a caption track into plain text. The
// timedtext baseUrl returns XML by default; each <text> cue is HTML-escaped
// (often double-escaped), so we unescape after XML decoding. The result is run
// through cleanSubtitleText for dedup + the shared length cap.
func fetchTimedText(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", nativeBrowserUA)
	req.Header.Set("Accept-Language", "ko,en;q=0.9")

	resp, err := httputil.NewClient(nativePlayerTimeout).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("timedtext HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	var doc timedTextDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse timedtext: %w", err)
	}

	var b strings.Builder
	for _, t := range doc.Texts {
		// xml decoded one escaping layer; YouTube double-escapes entities like
		// &amp;#39; so unescape once more to recover the real apostrophe etc.
		line := strings.TrimSpace(html.UnescapeString(t.Content))
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return cleanSubtitleText(b.String()), nil
}

// normalizeUploadDate converts an ISO-ish date ("2024-01-15", "2024-01-15T..."),
// to the "YYYYMMDD" form the rest of the package (formatUploadDate) expects.
func normalizeUploadDate(d string) string {
	var digits strings.Builder
	for _, c := range d {
		if c >= '0' && c <= '9' {
			digits.WriteRune(c)
		}
		if digits.Len() == 8 {
			break
		}
	}
	if digits.Len() == 8 {
		return digits.String()
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
