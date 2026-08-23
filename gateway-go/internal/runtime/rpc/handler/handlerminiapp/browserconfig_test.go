package handlerminiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func browserConfigHandler(t *testing.T, denebDir string) (func(ctx context.Context, method string) map[string]any, string) {
	t.Helper()
	methods := BrowserConfigMethods(BrowserConfigDeps{DenebDir: denebDir})
	h, ok := methods["miniapp.browser.config.get"]
	if !ok {
		t.Fatal("miniapp.browser.config.get not registered")
	}
	return func(ctx context.Context, method string) map[string]any {
		t.Helper()
		resp := h(ctx, newReq(t, method))
		return decodePayload(t, resp)
	}, denebDir
}

func TestBrowserConfig_AbsentFileServesEmptyNonNilLists(t *testing.T) {
	get, _ := browserConfigHandler(t, t.TempDir())
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := get(ctx, "miniapp.browser.config.get")

	for _, key := range []string{"adHostSuffixes", "adPathSegments", "adPathTokens", "adQueryMarkers", "quirks"} {
		raw, err := json.Marshal(got[key])
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		if string(raw) == "null" || string(raw) == "" {
			t.Errorf("%s = null, want [] (Kotlin non-null lists)", key)
		}
	}
	if v, _ := got["version"].(float64); v != 0 {
		t.Errorf("version = %v, want 0", got["version"])
	}
}

func TestBrowserConfig_EmptyDenebDirRegistersNothing(t *testing.T) {
	if methods := BrowserConfigMethods(BrowserConfigDeps{}); methods != nil {
		t.Fatalf("methods = %v, want nil", methods)
	}
}

func TestBrowserConfig_ValidFileRoundTrips(t *testing.T) {
	dir := t.TempDir()
	writeBrowserRules(t, dir, `{
		"version": 7,
		"adHostSuffixes": ["Ads.Example.com"],
		"adPathSegments": ["/banners/"],
		"adPathTokens": ["cdn.example/ads"],
		"adQueryMarkers": ["campaign_id="],
		"quirks": [{"hosts": ["example.com", "www.example.com"], "css": "body{overflow:auto !important}"}]
	}`)
	get, _ := browserConfigHandler(t, dir)
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := get(ctx, "miniapp.browser.config.get")

	if v, _ := got["version"].(float64); v != 7 {
		t.Errorf("version = %v, want 7", got["version"])
	}
	if hosts, _ := got["adHostSuffixes"].([]any); len(hosts) != 1 || hosts[0] != "ads.example.com" {
		t.Errorf("adHostSuffixes = %v, want lowercased [ads.example.com]", got["adHostSuffixes"])
	}
	if segs, _ := got["adPathSegments"].([]any); len(segs) != 1 || segs[0] != "/banners/" {
		t.Errorf("adPathSegments = %v", got["adPathSegments"])
	}
	quirks, _ := got["quirks"].([]any)
	if len(quirks) != 1 {
		t.Fatalf("quirks = %v, want 1", got["quirks"])
	}
	quirk, _ := quirks[0].(map[string]any)
	if hosts, _ := quirk["hosts"].([]any); len(hosts) != 2 {
		t.Errorf("quirk hosts = %v", quirk["hosts"])
	}
}

func TestBrowserConfig_InvalidEntriesAreDroppedNotFatal(t *testing.T) {
	dir := t.TempDir()
	writeBrowserRules(t, dir, `{
		"adHostSuffixes": ["good.example.com", "bad host with spaces", "", "UPPER.Example.com"],
		"adPathSegments": ["ok", "has`+"\\u0011"+`control", "`+strings.Repeat("x", 129)+`"],
		"quirks": [
			{"hosts": ["example.com"], "css": "body{}</style><script>alert(1)</script>"},
			{"hosts": [], "css": "body{}"},
			{"hosts": ["fine.example.com"], "css": "html{overflow:auto !important}"}
		]
	}`)
	get, _ := browserConfigHandler(t, dir)
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := get(ctx, "miniapp.browser.config.get")

	hosts, _ := got["adHostSuffixes"].([]any)
	if len(hosts) != 2 || hosts[0] != "good.example.com" || hosts[1] != "upper.example.com" {
		t.Errorf("adHostSuffixes = %v, want [good.example.com upper.example.com]", got["adHostSuffixes"])
	}
	if segs, _ := got["adPathSegments"].([]any); len(segs) != 1 {
		t.Errorf("adPathSegments = %v, want only the clean token", got["adPathSegments"])
	}
	quirks, _ := got["quirks"].([]any)
	if len(quirks) != 1 {
		t.Fatalf("quirks = %v, want only the clean quirk", got["quirks"])
	}
}

func TestBrowserConfig_MalformedFileDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	writeBrowserRules(t, dir, "{not json at all")
	get, _ := browserConfigHandler(t, dir)
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := get(ctx, "miniapp.browser.config.get")

	if hosts, _ := got["adHostSuffixes"].([]any); len(hosts) != 0 {
		t.Errorf("adHostSuffixes = %v, want empty", got["adHostSuffixes"])
	}
}

func TestBrowserConfig_StatCacheFollowsFileChangesAndDeletion(t *testing.T) {
	dir := t.TempDir()
	writeBrowserRules(t, dir, `{"version":1,"adHostSuffixes":["first.example.com"]}`)
	get, _ := browserConfigHandler(t, dir)
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	if got := get(ctx, "miniapp.browser.config.get"); got["version"].(float64) != 1 {
		t.Fatalf("first read version = %v", got["version"])
	}
	writeBrowserRules(t, dir, `{"version":2,"adHostSuffixes":["second.example.com"]}`)
	if got := get(ctx, "miniapp.browser.config.get"); got["version"].(float64) != 2 {
		t.Fatalf("after rewrite version = %v, want 2", got["version"])
	}
	if err := os.Remove(filepath.Join(dir, "browser-rules.json")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := get(ctx, "miniapp.browser.config.get")
	if v, _ := got["version"].(float64); v != 0 {
		t.Errorf("after delete version = %v, want 0 (stale cache served)", got["version"])
	}
}

func TestBrowserConfig_RequiresClientIdentity(t *testing.T) {
	resp := BrowserConfigMethods(BrowserConfigDeps{DenebDir: t.TempDir()})["miniapp.browser.config.get"]
	if resp == nil {
		t.Fatal("handler missing")
	}
	out := resp(context.Background(), newReq(t, "miniapp.browser.config.get"))
	if out.OK {
		t.Fatal("unauthenticated request must not be OK")
	}
}

func writeBrowserRules(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "browser-rules.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}
}
