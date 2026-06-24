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
	"regexp"
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
		Title            string   `json:"title"`
		Author           string   `json:"author"`
		ChannelID        string   `json:"channelId"`
		LengthSeconds    string   `json:"lengthSeconds"`
		ViewCount        string   `json:"viewCount"`
		ShortDescription string   `json:"shortDescription"`
		Keywords         []string `json:"keywords"`
		IsLiveContent    bool     `json:"isLiveContent"`
		Thumbnail        struct {
			Thumbnails []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"videoDetails"`
	Microformat struct {
		PlayerMicroformatRenderer struct {
			PublishDate     string `json:"publishDate"`
			UploadDate      string `json:"uploadDate"`
			Category        string `json:"category"`
			OwnerProfileURL string `json:"ownerProfileUrl"`
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

// ExtractYouTubeTranscriptNative runs ONLY the native innertube extraction
// (captions + chapters + metadata) — no yt-dlp subprocess and no audio→ASR. It
// is the lightweight primitive for inbound link enrichment, where spawning
// yt-dlp/ASR on every pasted link would be far too heavy for the synchronous
// send path. Returns nil when the native path can't serve the video (not
// playable, network/parse error); callers that need the full path (caption-less
// videos via ASR, alternate player clients) use ExtractYouTubeTranscript.
func ExtractYouTubeTranscriptNative(ctx context.Context, videoURL string) *YouTubeResult {
	return extractTranscriptNative(ctx, videoURL)
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
	micro := player.Microformat.PlayerMicroformatRenderer
	fullDesc := player.VideoDetails.ShortDescription

	result := &YouTubeResult{
		Title:       player.VideoDetails.Title,
		Channel:     player.VideoDetails.Author,
		ChannelID:   player.VideoDetails.ChannelID,
		ChannelURL:  channelURL(player.VideoDetails.ChannelID, micro.OwnerProfileURL),
		DurationSec: durSec,
		Duration:    formatDuration(durSec),
		UploadDate:  normalizeUploadDate(firstNonEmpty(micro.PublishDate, micro.UploadDate)),
		ViewCount:   views,
		Category:    micro.Category,
		Keywords:    player.VideoDetails.Keywords,
		IsLive:      player.VideoDetails.IsLiveContent,
		Thumbnail:   bestThumbnail(player.VideoDetails.Thumbnail.Thumbnails),
		Chapters:    parseChaptersFromDescription(fullDesc),
		Description: truncateString(fullDesc, 2000),
		URL:         videoURL,
	}

	tracks := player.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks
	result.AvailableCaptions = availableCaptionLabels(tracks)
	if baseURL, lang := selectCaptionTrack(tracks); baseURL != "" {
		if segs, err := fetchTimedText(ctx, baseURL); err == nil && len(segs) > 0 {
			result.Segments = segs
			result.Transcript = segmentsToPlainText(segs)
			result.Language = lang
		} else if err != nil {
			slog.Debug("native youtube caption fetch failed", "video", videoID, "lang", lang, "error", err)
		}
	}

	return result
}

// channelURL builds a canonical channel URL, preferring the explicit profile URL
// from the microformat and falling back to /channel/<id>.
func channelURL(channelID, profileURL string) string {
	if u := strings.TrimSpace(profileURL); u != "" {
		if strings.HasPrefix(u, "http") {
			return u
		}
		return "https://www.youtube.com" + u
	}
	if channelID != "" {
		return "https://www.youtube.com/channel/" + channelID
	}
	return ""
}

// bestThumbnail returns the highest-resolution thumbnail URL (largest area).
func bestThumbnail(thumbs []struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}) string {
	best, bestArea := "", -1
	for _, t := range thumbs {
		if area := t.Width * t.Height; area > bestArea && t.URL != "" {
			best, bestArea = t.URL, area
		}
	}
	return best
}

// availableCaptionLabels lists every caption track as a human label
// ("ko", "en (auto)", ...) so the metadata advertises what languages exist.
func availableCaptionLabels(tracks []captionTrack) []string {
	if len(tracks) == 0 {
		return nil
	}
	out := make([]string, 0, len(tracks))
	for _, t := range tracks {
		l := t.LanguageCode
		if l == "" {
			l = "unknown"
		}
		if t.Kind == "asr" {
			l += " (auto)"
		}
		out = append(out, l)
	}
	return out
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

// selectCaptionTrack picks a caption track. Preference (highest first):
// ko manual → en manual → ko auto → en auto → any manual → any auto. Language
// matching is by BCP-47 base tag, so regional variants (en-US, en-GB, ko-KR)
// count as their base language — matching yt-dlp's --sub-langs prefix behavior.
// Returns the track baseUrl and a human label ("ko", "en (auto)", "auto", ...).
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
			if lang != "" && baseLang(t.LanguageCode) != lang {
				continue
			}
			l := t.LanguageCode
			if l == "" {
				l = "unknown"
			}
			if auto {
				l += " (auto)" // keep the real language code (e.g. "ja (auto)")
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
	// No ko/en track — take any human (manual) caption before any auto one, so
	// a video whose only real captions are in another language still works.
	if u, l, ok := find("", false); ok {
		return u, l
	}
	if u, l, ok := find("", true); ok {
		return u, l
	}
	return "", ""
}

// baseLang returns the lowercase BCP-47 primary subtag ("en-US" -> "en").
func baseLang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexByte(code, '-'); i >= 0 {
		return code[:i]
	}
	return code
}

// timedTextDoc parses YouTube's timedtext caption XML. It handles BOTH shapes a
// caption baseUrl can return: the legacy `<transcript><text start=..>...` and the
// newer srv3 `<timedtext><body><p t=..><s>..</s></p>`. Decoding both means the
// native path doesn't silently discard a transcript when YouTube serves srv3.
type timedTextDoc struct {
	// Legacy: <text start="1.5" dur="2.0">caption</text>
	Texts []struct {
		Start   string `xml:"start,attr"`
		Dur     string `xml:"dur,attr"`
		Content string `xml:",chardata"`
	} `xml:"text"`
	// srv3: <body><p t="1500" d="2000"><s>cap</s><s>tion</s></p>
	Paras []srv3Para `xml:"body>p"`
}

type srv3Para struct {
	TMs     string `xml:"t,attr"`    // start in milliseconds
	Content string `xml:",chardata"` // text when there are no <s> runs
	Segs    []struct {
		Text string `xml:",chardata"`
	} `xml:"s"`
}

// fetchTimedText downloads a caption track and returns its cues as timestamped
// segments. The timedtext baseUrl returns XML by default; each <text> cue is
// HTML-escaped (often double-escaped), so we unescape after XML decoding.
func fetchTimedText(ctx context.Context, baseURL string) ([]TranscriptSegment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", nativeBrowserUA)
	req.Header.Set("Accept-Language", "ko,en;q=0.9")

	resp, err := httputil.NewClient(nativePlayerTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("timedtext HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var doc timedTextDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse timedtext: %w", err)
	}
	return doc.segments(), nil
}

// segments flattens whichever timedtext shape decoded (legacy <text> or srv3
// <p>) into timestamped cues. Entities are unescaped twice because YouTube
// double-escapes (xml decodes one layer; html.UnescapeString the second).
func (d timedTextDoc) segments() []TranscriptSegment {
	segs := make([]TranscriptSegment, 0, len(d.Texts)+len(d.Paras))
	for _, t := range d.Texts {
		line := strings.TrimSpace(html.UnescapeString(t.Content))
		if line == "" {
			continue
		}
		start, _ := strconv.ParseFloat(strings.TrimSpace(t.Start), 64)
		segs = append(segs, TranscriptSegment{StartSec: int(start), Text: line})
	}
	for _, p := range d.Paras {
		text := p.Content
		if len(p.Segs) > 0 {
			var sb strings.Builder
			for _, s := range p.Segs {
				sb.WriteString(s.Text)
			}
			text = sb.String()
		}
		line := strings.TrimSpace(html.UnescapeString(text))
		if line == "" {
			continue
		}
		ms, _ := strconv.ParseFloat(strings.TrimSpace(p.TMs), 64)
		segs = append(segs, TranscriptSegment{StartSec: int(ms / 1000), Text: line})
	}
	return segs
}

// segmentsToPlainText flattens caption segments into the plain deduped transcript
// (the summarizer's input), reusing cleanSubtitleText for dedup + the length cap.
func segmentsToPlainText(segs []TranscriptSegment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Text == "" {
			continue
		}
		b.WriteString(s.Text)
		b.WriteByte('\n')
	}
	return cleanSubtitleText(b.String())
}

// chapterLineRe matches a description chapter line: an optional bullet, a
// timestamp (m:ss / mm:ss / h:mm:ss), then the title.
var chapterLineRe = regexp.MustCompile(`(?m)^\s*(?:[-*•]\s*)?\(?(\d{1,2}:\d{2}(?::\d{2})?)\)?\s+[-–—)\].:]?\s*(.+?)\s*$`)

// parseChaptersFromDescription extracts YouTube chapters from the description's
// timestamp list. To avoid false positives it applies YouTube's own chapter
// rules: at least 3 markers, the first at 0:00, and non-decreasing offsets.
func parseChaptersFromDescription(desc string) []YouTubeChapter {
	matches := chapterLineRe.FindAllStringSubmatch(desc, -1)
	chapters := make([]YouTubeChapter, 0, len(matches))
	for _, m := range matches {
		title := strings.TrimSpace(m[2])
		if title == "" {
			continue
		}
		chapters = append(chapters, YouTubeChapter{StartSec: parseTimestampToSec(m[1]), Title: title})
	}
	if len(chapters) < 3 || chapters[0].StartSec != 0 {
		return nil
	}
	for i := 1; i < len(chapters); i++ {
		if chapters[i].StartSec < chapters[i-1].StartSec {
			return nil // not a monotonic chapter list — likely incidental timestamps
		}
	}
	return chapters
}

// parseTimestampToSec converts "m:ss", "mm:ss", or "h:mm:ss" to seconds.
func parseTimestampToSec(ts string) int {
	parts := strings.Split(ts, ":")
	secs := 0
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		secs = secs*60 + n
	}
	return secs
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
