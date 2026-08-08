package document

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDegenerateKeepsHonestTableExemptions(t *testing.T) {
	t.Parallel()

	// The 발주서 collapse: a full row block with real text repeating — R1.
	looped := strings.Repeat("Duck Joint(B/Jumper부착)\nH100 L Type\nEA\n433\n2,000\n", 15)
	if ocrDegenerate(looped, "") == "" {
		t.Fatal("degenerated row block must be flagged")
	}

	// Honest dense table: short unit/qty cells repeat a lot but INTERLEAVED
	// with varying item lines — the discriminator both repeat rules honor.
	var honest strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&honest, "케이블트레이 %d형\nEA\n%d\n12,000\n", i, i+1)
	}
	if why := ocrDegenerate(honest.String(), ""); why != "" {
		t.Fatalf("honest table with repeated short cells must NOT be flagged, got %q", why)
	}

	if ocrDegenerate("견적서\n합계 99,000원", "") != "" {
		t.Fatal("short prose must not be flagged")
	}

	// CHANGED CONTRACT (2026-08-02): ≥12 IDENTICAL CONSECUTIVE lines now flag
	// regardless of letters. The old guard exempted numeric-only repeats, and
	// that exact blind spot let "40"×1,020 (a whole score page) through AND
	// into the cache. A flattened honest column interleaves with its row's
	// other cells, so back-to-back identical ≥12 has no honest reading.
	if ocrDegenerate(strings.Repeat("866,000.00\n", 30), "") == "" {
		t.Fatal("30 consecutive identical lines are the collapse signature")
	}
}

func TestPaddleTableToText(t *testing.T) {
	t.Parallel()

	markup := "<fcel>월일<fcel>품명<fcel>금액<nl><fcel>7/17<fcel>안전관리수수료<lcel><fcel>90,000<nl><ucel><fcel>부가세<ecel><fcel>9,000<nl>"
	got := paddleTableToText(markup)
	want := "월일 | 품명 | 금액\n7/17 | 안전관리수수료 | 90,000\n부가세 | 9,000"
	if got != want {
		t.Fatalf("cell markup conversion:\n got %q\nwant %q", got, want)
	}

	// HTML-table answers reuse the existing markdown conversion path.
	if got := paddleTableToText("<table><tr><td>a</td><td>b</td></tr></table>"); !strings.Contains(got, "a") || strings.Contains(got, "<table") {
		t.Fatalf("html table must convert, got %q", got)
	}

	// Plain text passes through untouched.
	if got := paddleTableToText("그냥 텍스트"); got != "그냥 텍스트" {
		t.Fatalf("plain text changed: %q", got)
	}
}

// The loop → table-mode retry: first "OCR:" answer degenerates, the retry in
// "Table Recognition:" returns cell markup, and the caller receives the
// converted rows instead of the looped garbage (and never reaches tesseract).
func TestOCRImageBytesRetriesTableModeOnLoop(t *testing.T) {
	var tasks []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		task := req.Messages[0].Content[1].Text
		tasks = append(tasks, task)
		answer := strings.Repeat("Duck Joint(B/Jumper부착) H100 L Type\n", 20)
		if task == "Table Recognition:" {
			answer = "<fcel>품명<fcel>금액<nl><fcel>덕트조인트<fcel>866,000<nl>"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": answer}}},
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)
	t.Setenv("DENEB_OCR_CACHE_DIR", t.TempDir())

	got, err := ocrImageBytes(context.Background(), []byte("\x89PNG fake"))
	if err != nil {
		t.Fatalf("ocrImageBytes: %v", err)
	}
	if want := "품명 | 금액\n덕트조인트 | 866,000"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(tasks) != 2 || tasks[0] != "OCR:" || tasks[1] != "Table Recognition:" {
		t.Fatalf("task sequence = %v, want [OCR: Table Recognition:]", tasks)
	}
}

// finish_reason=length with shape-healthy text must not be cached when rescue
// fails — the budget signal is the degeneration, and shape-only checks miss it.
func TestOCRImageBytesDoesNotCacheTokenExhaustionWhenRescueFails(t *testing.T) {
	var b strings.Builder
	b.WriteString("Chapter 1\n\nCoherent document text.\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "Paragraph %d with distinct content.\n", i)
	}
	coherent := b.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		task := req.Messages[0].Content[1].Text
		content, finish := coherent, "length"
		if task == "Table Recognition:" {
			content = strings.Repeat("40\n", 20)
			finish = ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": content},
				"finish_reason": finish,
			}},
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)
	cacheDir := t.TempDir()
	t.Setenv("DENEB_OCR_CACHE_DIR", cacheDir)

	got, err := ocrImageBytes(context.Background(), []byte("\x89PNG length"))
	if err != nil {
		t.Fatalf("ocrImageBytes: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(coherent) {
		t.Fatalf("coherent last resort must survive, got %.40q", got)
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("token-exhausted output must stay uncached when rescue fails, found %d entries", len(entries))
	}
}

// A degenerated table-mode answer must not replace the original output.
func TestOCRImageBytesKeepsOriginalWhenRetryAlsoLoops(t *testing.T) {
	looped := strings.Repeat("Duck Joint(B/Jumper부착) H100 L Type\n", 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": looped}}},
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)
	cacheDir := t.TempDir()
	t.Setenv("DENEB_OCR_CACHE_DIR", cacheDir)

	got, err := ocrImageBytes(context.Background(), []byte("\x89PNG fake"))
	if err != nil {
		t.Fatalf("ocrImageBytes: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(looped) {
		t.Fatalf("looped original must survive as last resort, got %q", got[:40])
	}
	// A still-looped last resort must not be cached — a recovered server
	// should get to redo it.
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("looped output must stay uncached, found %d entries", len(entries))
	}
}

// A healthy result is cached by content hash: the second call for the same
// bytes never reaches the server.
func TestOCRImageBytesServesRepeatFromCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "합계 99,000원"}}},
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)
	t.Setenv("DENEB_OCR_CACHE_DIR", t.TempDir())

	for i := 0; i < 2; i++ {
		got, err := ocrImageBytes(context.Background(), []byte("\x89PNG cached"))
		if err != nil || got != "합계 99,000원" {
			t.Fatalf("call %d: got %q err %v", i, got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("server calls = %d, want 1 (second must hit the cache)", calls)
	}
	// Different bytes miss the cache.
	if _, err := ocrImageBytes(context.Background(), []byte("\x89PNG other")); err != nil {
		t.Fatalf("second image: %v", err)
	}
	if calls != 2 {
		t.Fatalf("server calls = %d, want 2 after a distinct image", calls)
	}
}

// The tesseract fallback path must not poison the cache: with the server down
// nothing is written, so a recovered server re-OCRs properly.
func TestOCRImageBytesDoesNotCacheTesseractFallback(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("DENEB_OCR_VL_URL", "http://127.0.0.1:1") // refused instantly
	t.Setenv("DENEB_OCR_CACHE_DIR", cacheDir)

	_, _ = ocrImageBytes(context.Background(), []byte("\x89PNG down"))
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("fallback output must stay uncached, found %d entries", len(entries))
	}
}

// Live gate (skipped by default): reproduces the real loop page against the
// production PaddleOCR-VL server and asserts the fallback yields non-looped
// text. Run: DENEB_OCR_LIVE_PAGE=~/ocr-eval/pages/d2_발주서-1.png go test -run Live ./...
func TestOCRImageBytesLoopFallback_Live(t *testing.T) {
	page := os.Getenv("DENEB_OCR_LIVE_PAGE")
	if page == "" {
		t.Skip("set DENEB_OCR_LIVE_PAGE to a looping page PNG to run")
	}
	img, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	// Isolate from the real cache so reruns keep exercising the loop path.
	t.Setenv("DENEB_OCR_CACHE_DIR", t.TempDir())
	got, err := ocrImageBytes(context.Background(), img)
	if err != nil {
		t.Fatalf("ocrImageBytes: %v", err)
	}
	if why := ocrDegenerate(got, ""); why != "" {
		t.Fatalf("live fallback still degenerate (%s, %d chars)", why, len(got))
	}
	t.Logf("live output %d chars, head: %.120s", len(got), got)
}
