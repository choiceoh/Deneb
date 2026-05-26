package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// signFixture builds a Telegram-format init_data query string signed with
// botToken. The fields map is sent as-is (URL-encoded once). The returned
// string is what Telegram itself would deliver via Telegram.WebApp.initData.
func signFixture(t *testing.T, botToken string, fields map[string]string) string {
	t.Helper()

	keys := make([]string, 0, len(fields))
	for k := range fields {
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
		sb.WriteString(fields[k])
	}
	dataCheck := sb.String()

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(botToken))
	secret := secretMAC.Sum(nil)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(dataCheck))
	hash := hex.EncodeToString(mac.Sum(nil))

	values := url.Values{}
	for k, v := range fields {
		values.Set(k, v)
	}
	values.Set("hash", hash)
	return values.Encode()
}

func TestVerifyInitData_GoodSignature(t *testing.T) {
	const botToken = "123456:test-token"
	now := time.Now().UTC().Unix()
	raw := signFixture(t, botToken, map[string]string{
		"auth_date":     strconv.FormatInt(now, 10),
		"query_id":      "AAH123",
		"user":          `{"id":42,"first_name":"Peter","username":"peter","language_code":"ko"}`,
		"chat_type":     "private",
		"chat_instance": "chat-instance-1",
	})

	data, err := VerifyInitData(raw, botToken, 0)
	if err != nil {
		t.Fatalf("VerifyInitData: %v", err)
	}
	if data.User == nil {
		t.Fatalf("user not decoded")
	}
	if data.User.ID != 42 || data.User.Username != "peter" || data.User.LanguageCode != "ko" {
		t.Fatalf("user fields wrong: %+v", data.User)
	}
	if data.QueryID != "AAH123" {
		t.Fatalf("query_id wrong: %s", data.QueryID)
	}
	if data.ChatType != "private" {
		t.Fatalf("chat_type wrong: %s", data.ChatType)
	}
	if data.AuthDate.Unix() != now {
		t.Fatalf("auth_date wrong: %d vs %d", data.AuthDate.Unix(), now)
	}
}

func TestVerifyInitData_TamperedHash(t *testing.T) {
	const botToken = "123456:test-token"
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	raw := signFixture(t, botToken, map[string]string{
		"auth_date": now,
		"user":      `{"id":42}`,
	})

	// Flip one byte in the hash.
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	h := values.Get("hash")
	flipped := h[:len(h)-1]
	if h[len(h)-1] == '0' {
		flipped += "1"
	} else {
		flipped += "0"
	}
	values.Set("hash", flipped)

	_, err = VerifyInitData(values.Encode(), botToken, 0)
	if !errors.Is(err, ErrInitDataBadHash) {
		t.Fatalf("expected ErrInitDataBadHash, got %v", err)
	}
}

func TestVerifyInitData_TamperedField(t *testing.T) {
	const botToken = "123456:test-token"
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	raw := signFixture(t, botToken, map[string]string{
		"auth_date": now,
		"user":      `{"id":42}`,
	})

	// Replace user payload after signing — signature must reject it.
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("user", `{"id":9999,"first_name":"attacker"}`)

	_, err = VerifyInitData(values.Encode(), botToken, 0)
	if !errors.Is(err, ErrInitDataBadHash) {
		t.Fatalf("expected ErrInitDataBadHash for tampered field, got %v", err)
	}
}

func TestVerifyInitData_WrongToken(t *testing.T) {
	const realToken = "123456:real"
	const wrongToken = "123456:wrong"
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	raw := signFixture(t, realToken, map[string]string{
		"auth_date": now,
		"user":      `{"id":42}`,
	})

	_, err := VerifyInitData(raw, wrongToken, 0)
	if !errors.Is(err, ErrInitDataBadHash) {
		t.Fatalf("expected ErrInitDataBadHash for wrong token, got %v", err)
	}
}

func TestVerifyInitData_Expired(t *testing.T) {
	const botToken = "123456:test-token"
	old := strconv.FormatInt(time.Now().Add(-48*time.Hour).Unix(), 10)
	raw := signFixture(t, botToken, map[string]string{
		"auth_date": old,
		"user":      `{"id":42}`,
	})

	_, err := VerifyInitData(raw, botToken, 1*time.Hour)
	if !errors.Is(err, ErrInitDataExpired) {
		t.Fatalf("expected ErrInitDataExpired, got %v", err)
	}

	// Negative TTL disables freshness check — same fixture must verify.
	if _, err := VerifyInitData(raw, botToken, -1); err != nil {
		t.Fatalf("expected freshness check skipped, got %v", err)
	}
}

func TestVerifyInitData_Empty(t *testing.T) {
	_, err := VerifyInitData("", "123456:test", 0)
	if !errors.Is(err, ErrInitDataEmpty) {
		t.Fatalf("expected ErrInitDataEmpty, got %v", err)
	}
}

func TestVerifyInitData_MissingHash(t *testing.T) {
	raw := url.Values{
		"auth_date": []string{"1700000000"},
		"user":      []string{`{"id":42}`},
	}.Encode()

	_, err := VerifyInitData(raw, "123456:test", -1)
	if !errors.Is(err, ErrInitDataMissingHash) {
		t.Fatalf("expected ErrInitDataMissingHash, got %v", err)
	}
}

func TestVerifyInitData_NoBotToken(t *testing.T) {
	_, err := VerifyInitData("hash=abc", "", 0)
	if err == nil || !strings.Contains(err.Error(), "bot token required") {
		t.Fatalf("expected bot token required error, got %v", err)
	}
}

func TestVerifyInitData_MissingAuthDate(t *testing.T) {
	const botToken = "123456:test-token"
	raw := signFixture(t, botToken, map[string]string{
		"user": `{"id":42}`,
	})

	_, err := VerifyInitData(raw, botToken, -1)
	if !errors.Is(err, ErrInitDataNoAuthDate) {
		t.Fatalf("expected ErrInitDataNoAuthDate, got %v", err)
	}
}

func TestVerifyInitData_StartParam(t *testing.T) {
	const botToken = "123456:test-token"
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	raw := signFixture(t, botToken, map[string]string{
		"auth_date":   now,
		"user":        `{"id":42}`,
		"start_param": "inbox_overview",
	})

	data, err := VerifyInitData(raw, botToken, 0)
	if err != nil {
		t.Fatal(err)
	}
	if data.StartParam != "inbox_overview" {
		t.Fatalf("start_param wrong: %s", data.StartParam)
	}
	if data.Raw["start_param"] != "inbox_overview" {
		t.Fatalf("Raw[start_param] wrong: %s", data.Raw["start_param"])
	}
	if _, hasHash := data.Raw["hash"]; hasHash {
		t.Fatalf("Raw must not include the hash field")
	}
}
