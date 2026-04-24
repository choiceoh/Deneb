// Copyright (c) Deneb authors. Licensed under the project license.

package redact

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// Test-only helper: run a function with `enabled` forced to a value, then
// restore. Safe because the tests run sequentially (no t.Parallel on tests
// that touch the flag).
func withEnabled(t *testing.T, v bool, fn func()) {
	t.Helper()
	prev := enabled
	enabled = v
	defer func() { enabled = prev }()
	fn()
}

func TestParseEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", true},
		{"1", true},
		{"true", true},
		{"YES", true},
		{"on", true},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{" off ", false},
	}
	for _, c := range cases {
		if got := parseEnabled(c.raw); got != c.want {
			t.Errorf("parseEnabled(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestString_VendorPrefixes(t *testing.T) {
	cases := []struct {
		name   string
		token  string // constructed at runtime so no literal secret appears in source
		prefix string // first chars the mask must preserve (pattern: "<first6>...<last4>")
	}{
		{name: "openai_sk_dash", token: "sk-proj-" + strings.Repeat("Z", 24), prefix: "sk-pro"},
		{name: "github_ghp", token: "ghp_" + strings.Repeat("Z", 36), prefix: "ghp_ZZ"},
		{name: "github_gho", token: "gho_" + strings.Repeat("Z", 36), prefix: "gho_ZZ"},
		{name: "github_ghu", token: "ghu_" + strings.Repeat("Z", 36), prefix: "ghu_ZZ"},
		{name: "github_fine_grained", token: "github_pat_11" + strings.Repeat("Z", 50), prefix: "github"},
		{name: "google_aiza", token: "AIzaSy" + strings.Repeat("Z", 33), prefix: "AIzaSy"},
		{name: "aws_akia", token: "AKIA" + strings.Repeat("Z", 16), prefix: "AKIAZZ"},
		{name: "huggingface_hf", token: "hf_" + strings.Repeat("Z", 32), prefix: "hf_ZZZ"},
		{name: "replicate_r8", token: "r8_" + strings.Repeat("Z", 32), prefix: "r8_ZZZ"},
		{name: "perplexity_pplx", token: "pplx-" + strings.Repeat("Z", 32), prefix: "pplx-Z"},
		{name: "xai", token: "xai-" + strings.Repeat("Z", 32), prefix: "xai-ZZ"},
		{name: "nvidia_nvapi", token: "nvapi-" + strings.Repeat("Z", 32), prefix: "nvapi-"},
		{name: "slack_xoxb", token: "xoxb-" + strings.Repeat("1", 11) + "-" + strings.Repeat("Z", 24), prefix: "xoxb-1"},
		{name: "stripe_sk_live", token: "sk_live_" + strings.Repeat("Z", 24), prefix: "sk_liv"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := "prefix " + c.token + " suffix"
			got := String(in)
			if strings.Contains(got, c.token) {
				t.Errorf("output still contains full token:\n  in:  %q\n  out: %q", in, got)
			}
			if !strings.Contains(got, c.prefix) {
				t.Errorf("output missing expected masked prefix %q:\n  in:  %q\n  out: %q", c.prefix, in, got)
			}
		})
	}
}

func TestString_JWT(t *testing.T) {
	// Runtime-constructed three-segment JWT: eyJ… header . eyJ… payload . signature
	// Segments are base64-url-safe padding so pattern matches without literal secret in source.
	jwt := "eyJ" + strings.Repeat("Z", 30) + "." + "eyJ" + strings.Repeat("Z", 30) + "." + strings.Repeat("Z", 20)
	in := "token: " + jwt + " trailing"
	got := String(in)
	if strings.Contains(got, jwt) {
		t.Errorf("JWT not masked: %q", got)
	}
	if !strings.Contains(got, "eyJZZZ") {
		t.Errorf("JWT prefix not preserved: %q", got)
	}
}

func TestString_DBConnString(t *testing.T) {
	cases := []string{
		"postgres://user:s3cret@host:5432/db",
		"postgresql://user:s3cret@host:5432/db",
		"mysql://root:supersecret@db.local/app",
		"mongodb://admin:hunter2@mongo.svc",
		"mongodb+srv://admin:hunter2@mongo.svc/?ssl=true",
		"redis://user:REDIS_PASSWORD_HERE@redis:6379",
		"amqp://guest:guestpass@rabbit:5672/",
	}
	for _, in := range cases {
		got := String(in)
		if strings.Contains(got, ":s3cret@") || strings.Contains(got, ":supersecret@") ||
			strings.Contains(got, ":hunter2@") || strings.Contains(got, ":REDIS_PASSWORD_HERE@") ||
			strings.Contains(got, ":guestpass@") {
			t.Errorf("DB password not masked: %q -> %q", in, got)
		}
		if !strings.Contains(got, ":***@") {
			t.Errorf("expected :***@ in output: %q -> %q", in, got)
		}
	}
}

func TestString_AuthHeaders(t *testing.T) {
	// Bearer token: construct at runtime to avoid static secret detection.
	bearerTok := "sk-proj-" + strings.Repeat("Z", 24)
	got := String("Authorization: Bearer " + bearerTok)
	if strings.Contains(got, bearerTok) {
		t.Errorf("bearer token leaked: %q", got)
	}
	apiKeyTok := "ghp_" + strings.Repeat("Z", 26)
	got = String("X-API-Key: " + apiKeyTok)
	if strings.Contains(got, apiKeyTok) {
		t.Errorf("x-api-key token leaked: %q", got)
	}
}

func TestString_URLQueryParams(t *testing.T) {
	cases := []struct {
		in       string
		wantGone string
		wantIn   string
	}{
		{
			in:       "https://example.com/cb?code=ABC123XYZ&state=keepme",
			wantGone: "code=ABC123XYZ",
			wantIn:   "code=***",
		},
		{
			in:       "https://api.host.dev/v1/foo?access_token=opaque_token_123&limit=10",
			wantGone: "access_token=opaque_token_123",
			wantIn:   "access_token=***",
		},
		{
			in:       "https://signed.example.com/r?x-amz-signature=SIG123&expires=9999",
			wantGone: "x-amz-signature=SIG123",
			wantIn:   "x-amz-signature=***",
		},
	}
	for _, c := range cases {
		got := String(c.in)
		if strings.Contains(got, c.wantGone) {
			t.Errorf("expected %q to be redacted: %q", c.wantGone, got)
		}
		if !strings.Contains(got, c.wantIn) {
			t.Errorf("expected %q in output: %q", c.wantIn, got)
		}
	}
}

func TestString_URLUserinfo(t *testing.T) {
	got := String("https://admin:supersecret@api.example.com/x")
	if strings.Contains(got, "supersecret") {
		t.Errorf("URL userinfo password leaked: %q", got)
	}
	if !strings.Contains(got, "admin:***@") {
		t.Errorf("expected admin:***@ in output: %q", got)
	}
}

func TestString_FormBody(t *testing.T) {
	in := "grant_type=password&username=alice&password=hunter2&scope=read"
	got := String(in)
	if strings.Contains(got, "hunter2") {
		t.Errorf("form body password leaked: %q", got)
	}
	if !strings.Contains(got, "password=***") {
		t.Errorf("expected password=*** in output: %q", got)
	}
	// Non-form text should pass through.
	passThrough := "hello world\npassword=is_not_form"
	if got := String(passThrough); got != passThrough {
		// This input still triggers the JSON-keyword prefilter and the
		// phone detector might not touch it; only form-body logic should
		// be a no-op here. We allow any transformation that does NOT
		// drop the `is_not_form` literal.
		if !strings.Contains(got, "is_not_form") {
			t.Errorf("unexpected transformation of non-form text: %q -> %q", passThrough, got)
		}
	}
}

func TestString_JSONFields(t *testing.T) {
	apiKey := strings.Repeat("Z", 32) // synthetic 32-char token; no literal secret in source
	in := `{"apiKey": "` + apiKey + `", "other": "keep"}`
	got := String(in)
	if strings.Contains(got, apiKey) {
		t.Errorf("JSON apiKey value leaked: %q", got)
	}
	if !strings.Contains(got, `"other": "keep"`) {
		t.Errorf("non-sensitive field mangled: %q", got)
	}
}

func TestString_EnvAssignment(t *testing.T) {
	secret := "sk-" + strings.Repeat("Z", 24) // runtime-built, no literal secret in source
	in := "OPENAI_API_KEY=" + secret + " trailing"
	got := String(in)
	if strings.Contains(got, secret) {
		t.Errorf("env value leaked: %q", got)
	}
	if !strings.Contains(got, "OPENAI_API_KEY=") {
		t.Errorf("env name mangled: %q", got)
	}
}

func TestString_PrivateKeyBlock(t *testing.T) {
	in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEvQIBADANBgkqh...lots of base64...\n-----END RSA PRIVATE KEY-----\nafter"
	got := String(in)
	if strings.Contains(got, "MIIEvQIBADAN") {
		t.Errorf("private key body leaked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
		t.Errorf("expected placeholder in output: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding context lost: %q", got)
	}
}

func TestString_TelegramBotToken(t *testing.T) {
	// Telegram bot token format: bot<digits>:<35-char base64url>. Constructed
	// at runtime so no literal token appears in the source.
	tokBody := "AAH" + strings.Repeat("Z", 32)
	in := "bot123456789:" + tokBody
	got := String(in)
	if strings.Contains(got, tokBody) {
		t.Errorf("telegram token leaked: %q", got)
	}
	if !strings.Contains(got, "bot123456789:***") {
		t.Errorf("expected bot<digits>:*** in output: %q", got)
	}
}

func TestString_DiscordMention(t *testing.T) {
	got := String("hey <@123456789012345678> look")
	if strings.Contains(got, "123456789012345678") {
		t.Errorf("discord ID leaked: %q", got)
	}
	if !strings.Contains(got, "<@***>") {
		t.Errorf("expected <@***> in output: %q", got)
	}
	got = String("<@!123456789012345678>")
	if !strings.Contains(got, "<@!***>") {
		t.Errorf("expected <@!***> in output: %q", got)
	}
}

func TestString_Phone(t *testing.T) {
	got := String("call me at +14155551234 tomorrow")
	if strings.Contains(got, "14155551234") {
		t.Errorf("phone number leaked: %q", got)
	}
}

func TestString_NegativeCases(t *testing.T) {
	// Plain text that must pass through unchanged.
	cases := []string{
		"Hello, world!",
		"The quick brown fox jumps over the lazy dog.",
		"사용자 이름을 입력하세요.",
		"file count: 42, bytes: 1024",
		"notsk-foobar embedded in identifier",   // vendor prefix inside identifier must not match
		"GHOSTWRITER is the name of my band",    // looks like gho_ but lacks underscore-delimited form
		"config key=value for generic settings", // no secret keyword
	}
	for _, in := range cases {
		got := String(in)
		if got != in {
			t.Errorf("negative case transformed:\n  in:  %q\n  out: %q", in, got)
		}
	}
}

func TestString_Idempotency(t *testing.T) {
	skTok := "sk-proj-" + strings.Repeat("Z", 24)
	ghpTok := "ghp_" + strings.Repeat("Z", 32)
	jsonTok := strings.Repeat("Z", 28)
	jwtTok := "eyJ" + strings.Repeat("Z", 30) + "." + "eyJ" + strings.Repeat("Z", 30) + "." + strings.Repeat("Z", 20)
	cases := []string{
		skTok,
		"postgres://u:p@h/d?api_key=foo",
		`{"token": "` + jsonTok + `"}`,
		"Authorization: Bearer " + ghpTok,
		"hello world",
		jwtTok,
	}
	for _, in := range cases {
		once := String(in)
		twice := String(once)
		if once != twice {
			t.Errorf("not idempotent:\n  in:    %q\n  once:  %q\n  twice: %q", in, once, twice)
		}
	}
}

func TestString_EmptyAndDisabled(t *testing.T) {
	if got := String(""); got != "" {
		t.Errorf("empty input should return empty: %q", got)
	}
	withEnabled(t, false, func() {
		in := "sk-proj-" + strings.Repeat("Z", 24)
		if got := String(in); got != in {
			t.Errorf("disabled should pass through: in=%q got=%q", in, got)
		}
	})
}

func TestBytes(t *testing.T) {
	// Empty input returns same slice (no alloc).
	if got := Bytes(nil); got != nil {
		t.Errorf("nil should return nil, got %v", got)
	}
	empty := []byte{}
	if got := Bytes(empty); len(got) != 0 {
		t.Errorf("empty should return empty, got %v", got)
	}
	// Non-matching input returns the same backing array.
	in := []byte("hello world")
	got := Bytes(in)
	if &got[0] != &in[0] {
		t.Errorf("non-matching input should return same backing array")
	}
	// Matching input is redacted.
	in = []byte("token: sk-proj-REDACTFIXTUREREDACT00000")
	got = Bytes(in)
	if bytes.Contains(got, []byte("sk-proj-REDACTFIXTUREREDACT00000")) {
		t.Errorf("bytes not redacted: %q", got)
	}
}

func TestAttrReplacer_StringValue(t *testing.T) {
	r := AttrReplacer(nil)
	a := slog.String("token", "sk-proj-REDACTFIXTUREREDACT00000")
	out := r(nil, a)
	if strings.Contains(out.Value.String(), "sk-proj-REDACTFIXTUREREDACT00000") {
		t.Errorf("slog string attr not redacted: %q", out.Value.String())
	}
}

func TestAttrReplacer_ErrorValue(t *testing.T) {
	r := AttrReplacer(nil)
	err := errors.New("call failed: Bearer sk-proj-REDACTFIXTUREREDACT00000")
	a := slog.Any("error", err)
	out := r(nil, a)
	got := out.Value.String()
	if strings.Contains(got, "sk-proj-REDACTFIXTUREREDACT00000") {
		t.Errorf("slog error attr not redacted: %q", got)
	}
}

func TestAttrReplacer_PrevChain(t *testing.T) {
	// prev replacer tags every Attr with a known sentinel so we can
	// verify the chain order: prev first, then our redaction.
	prev := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "x" {
			a.Value = slog.StringValue("prev:" + a.Value.String())
		}
		return a
	}
	r := AttrReplacer(prev)
	a := slog.String("x", "sk-proj-REDACTFIXTUREREDACT00000")
	out := r(nil, a)
	got := out.Value.String()
	if !strings.HasPrefix(got, "prev:") {
		t.Errorf("prev not applied first: %q", got)
	}
	if strings.Contains(got, "sk-proj-REDACTFIXTUREREDACT00000") {
		t.Errorf("redaction not applied after prev: %q", got)
	}
}

func TestAttrReplacer_NonStringPassthrough(t *testing.T) {
	r := AttrReplacer(nil)
	a := slog.Int("count", 42)
	out := r(nil, a)
	if out.Value.Int64() != 42 {
		t.Errorf("int attr mutated: %v", out.Value.Int64())
	}
}

func TestAttrReplacer_FullHandlerIntegration(t *testing.T) {
	// Hook the redactor into a real slog handler and verify the rendered
	// output does not contain the raw secret.
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: AttrReplacer(nil),
	})
	logger := slog.New(h)
	logger.Info("hello", "key", "sk-proj-REDACTFIXTUREREDACT00000")
	out := buf.String()
	if strings.Contains(out, "sk-proj-REDACTFIXTUREREDACT00000") {
		t.Errorf("handler output leaked secret: %q", out)
	}
}

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "***"},
		{"short", "***"},
		{"exactly17charact_", "***"},                  // len 17 < 18
		{"eighteenchars_here", "eighte...here"},       // len 18
		{"sk-proj-abcdef1234567890", "sk-pro...7890"}, // typical
		{"a-very-long-token-that-goes-on", "a-very...s-on"},
	}
	for _, c := range cases {
		if got := maskToken(c.in); got != c.want {
			t.Errorf("maskToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsTokenBoundary(t *testing.T) {
	s := "abc-def.ghi"
	// Out-of-range indices are boundaries.
	if !isTokenBoundary(s, -1) {
		t.Error("-1 should be boundary")
	}
	if !isTokenBoundary(s, len(s)) {
		t.Error("len should be boundary")
	}
	// '.' is a boundary.
	if !isTokenBoundary(s, 7) { // position of '.'
		t.Errorf("'.' should be boundary")
	}
	// '-' inside identifier is NOT a boundary (part of extended token).
	if isTokenBoundary(s, 3) { // position of '-'
		t.Errorf("'-' should not be boundary")
	}
	// Letter is not a boundary.
	if isTokenBoundary(s, 0) {
		t.Errorf("'a' should not be boundary")
	}
}

// Fuzz-style smoke check: verify String never panics on a variety of
// pathological inputs. This is not a real fuzz target (kept out of the
// corpus) but catches obvious regex / slicing bugs during unit runs.
func TestString_PathologicalInputs(t *testing.T) {
	inputs := []string{
		"",
		"\x00",
		strings.Repeat("=", 10000),
		strings.Repeat("&", 10000),
		strings.Repeat("sk-", 1000),
		strings.Repeat("eyJ", 100) + "." + strings.Repeat("abc", 100),
		"https://" + strings.Repeat("a", 1000) + "?code=" + strings.Repeat("b", 1000),
		"<@" + strings.Repeat("1", 30) + ">",
	}
	for _, in := range inputs {
		// Must not panic.
		_ = String(in)
	}
}
