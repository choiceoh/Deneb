package opstranslate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 수동 프로브: 실제 DeepL 키가 있을 때만 돈다. go test -run LiveProbe -v
func TestLiveProbe(t *testing.T) {
	if os.Getenv("OPSTRANSLATE_LIVE") == "" {
		t.Skip("set OPSTRANSLATE_LIVE=1")
	}
	dir := t.TempDir()
	store.mu.Lock()
	store.pathOverride = filepath.Join(dir, "c.json")
	store.loaded = false
	store.entries = nil
	store.order = nil
	store.mu.Unlock()

	texts := []string{
		"Promote rejected evolve into held-out validation",
		"Rejected evolve should become a validation case for 12+ exec calls wasted on /tmp filesystem scan when no file attached",
		"agentlog tool-latency signal — tool slower than its per-tool ceiling or regressed vs its baseline (RSI surface)",
		"Guardrail working as designed — broad rewrite correctly rejected by patch-first gate. No skill defect to fix.",
		"Do not auto-apply the rejected body; only convert stable observed behavior into a test/replay assertion.",
		"이미 한글인 문장은 그대로 남아야 한다 — 번역하면 품질만 잃는다.",
		`{"candidate":"email-analysis-full","score":5.9}`,
		"cron:morning-letter:1780959600105",
		"observe.behavior 7d vs 30d baseline: mail_archive calls=113 avgMs=750 ceiling=2500",
	}
	t0 := time.Now()
	out := Fields(context.Background(), texts)
	cold := time.Since(t0)
	for i, s := range texts {
		mark := "그대로"
		if out[i] != s {
			mark = "번역됨"
		}
		t.Logf("[%s] %s\n        → %s", mark, trunc(s), trunc(out[i]))
	}
	Flush()

	t1 := time.Now()
	out2 := Fields(context.Background(), texts)
	warm := time.Since(t1)
	for i := range out {
		if out[i] != out2[i] {
			t.Errorf("두 번째 호출이 다름: %q vs %q", out[i], out2[i])
		}
	}
	blob, _ := os.ReadFile(store.path())
	var f cacheFile
	_ = json.Unmarshal(blob, &f)
	t.Logf("콜드 %v · 웜 %v · 디스크 캐시 항목 %d개 · 파일 %d바이트", cold.Round(time.Millisecond), warm.Round(time.Millisecond), len(f.Entries), len(blob))
	if strings.Contains(string(blob), "Promote rejected evolve") {
		t.Error("원문이 캐시 파일에 저장됐다 — 해시 키만 저장해야 한다")
	}
}

func trunc(s string) string {
	r := []rune(s)
	if len(r) > 70 {
		return string(r[:70]) + "…"
	}
	return s
}
