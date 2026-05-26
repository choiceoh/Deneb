package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// InitData represents the parsed and verified Telegram WebApp launch payload.
// See https://core.telegram.org/bots/webapps#webappinitdata.
//
// Only the fields the gateway currently needs are decoded; unknown query
// parameters are preserved in Raw so callers can inspect them if necessary.
type InitData struct {
	// QueryID is the unique session identifier for this launch.
	QueryID string
	// User is the Telegram user that launched the mini app. May be nil for
	// channel-bot launches (which the gateway does not accept).
	User *WebAppUser
	// AuthDate is the unix second timestamp the init data was signed by Telegram.
	AuthDate time.Time
	// StartParam is the optional payload passed via t.me/<bot>?startapp=<value>.
	StartParam string
	// ChatType is "sender", "private", "group", "supergroup", or "channel".
	ChatType string
	// ChatInstance is an opaque identifier shared across launches in the same chat.
	ChatInstance string
	// Raw is the full decoded query map (excluding "hash") for diagnostic use.
	Raw map[string]string
}

// WebAppUser is the Telegram user object embedded in init data.
type WebAppUser struct {
	ID              int64  `json:"id"`
	IsBot           bool   `json:"is_bot,omitempty"`
	FirstName       string `json:"first_name,omitempty"`
	LastName        string `json:"last_name,omitempty"`
	Username        string `json:"username,omitempty"`
	LanguageCode    string `json:"language_code,omitempty"`
	IsPremium       bool   `json:"is_premium,omitempty"`
	AllowsWriteToPM bool   `json:"allows_write_to_pm,omitempty"`
	PhotoURL        string `json:"photo_url,omitempty"`
}

// Errors returned by VerifyInitData. Callers should compare with errors.Is.
var (
	ErrInitDataEmpty       = errors.New("telegram: empty init data")
	ErrInitDataMissingHash = errors.New("telegram: init data missing hash")
	ErrInitDataBadHash     = errors.New("telegram: init data signature mismatch")
	ErrInitDataNoAuthDate  = errors.New("telegram: init data missing auth_date")
	ErrInitDataExpired     = errors.New("telegram: init data expired")
	ErrInitDataNoUser      = errors.New("telegram: init data missing user")
)

// DefaultInitDataTTL bounds how long a signed init data payload is accepted
// after Telegram issued it. 24 hours mirrors Telegram's documented guidance —
// shorter would force the mini app to re-launch during long sessions.
const DefaultInitDataTTL = 24 * time.Hour

// VerifyInitData parses and authenticates a Telegram WebApp initData string
// signed with botToken. The HMAC scheme is documented at
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app.
//
// ttl bounds how stale the signed payload may be. Pass 0 to use
// DefaultInitDataTTL. Pass a negative value to disable the freshness check
// (recommended only for offline test fixtures).
func VerifyInitData(rawInitData, botToken string, ttl time.Duration) (*InitData, error) {
	if rawInitData == "" {
		return nil, ErrInitDataEmpty
	}
	if botToken == "" {
		return nil, errors.New("telegram: bot token required for init data verification")
	}

	values, err := url.ParseQuery(rawInitData)
	if err != nil {
		return nil, fmt.Errorf("telegram: parse init data: %w", err)
	}

	suppliedHash := values.Get("hash")
	if suppliedHash == "" {
		return nil, ErrInitDataMissingHash
	}

	// Build the data-check string: every key (except "hash") in lexicographic
	// order, joined with newlines, using the URL-decoded value.
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(values.Get(k))
	}
	dataCheck := sb.String()

	// Secret key = HMAC-SHA256("WebAppData", botToken).
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(botToken))
	secret := secretMAC.Sum(nil)

	// hash = hex(HMAC-SHA256(secret, dataCheck)).
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(dataCheck))
	expected := mac.Sum(nil)

	suppliedBytes, err := hex.DecodeString(suppliedHash)
	if err != nil {
		return nil, ErrInitDataBadHash
	}
	if !hmac.Equal(expected, suppliedBytes) {
		return nil, ErrInitDataBadHash
	}

	// Signature OK — now decode the structured fields.
	raw := make(map[string]string, len(keys))
	for _, k := range keys {
		raw[k] = values.Get(k)
	}

	authDateStr := values.Get("auth_date")
	if authDateStr == "" {
		return nil, ErrInitDataNoAuthDate
	}
	authSecs, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid auth_date: %w", err)
	}
	authDate := time.Unix(authSecs, 0).UTC()

	effectiveTTL := ttl
	if effectiveTTL == 0 {
		effectiveTTL = DefaultInitDataTTL
	}
	if effectiveTTL > 0 {
		if age := time.Since(authDate); age > effectiveTTL {
			return nil, fmt.Errorf("%w: age=%s ttl=%s", ErrInitDataExpired, age.Round(time.Second), effectiveTTL)
		}
	}

	out := &InitData{
		QueryID:      values.Get("query_id"),
		AuthDate:     authDate,
		StartParam:   values.Get("start_param"),
		ChatType:     values.Get("chat_type"),
		ChatInstance: values.Get("chat_instance"),
		Raw:          raw,
	}

	if userJSON := values.Get("user"); userJSON != "" {
		var user WebAppUser
		if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
			return nil, fmt.Errorf("telegram: decode user: %w", err)
		}
		out.User = &user
	}

	return out, nil
}
