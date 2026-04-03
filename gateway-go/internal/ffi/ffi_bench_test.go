package ffi

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
)

// --- Protocol validation (hot path: every inbound RPC request) ---

func BenchmarkValidateFrame(b *testing.B) {
	frame := `{"type":"req","id":"r-1","method":"session.list","params":{}}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateFrame(frame)
	}
}

func BenchmarkValidateFrame_Invalid(b *testing.B) {
	frame := `{"type":"req","id":"","method":""}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateFrame(frame)
	}
}

// --- Session key validation (hot path: auth on every request) ---

func BenchmarkValidateSessionKey(b *testing.B) {
	key := "direct:telegram:123456789"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateSessionKey(key)
	}
}

// --- Constant-time comparison (hot path: token auth) ---

func BenchmarkConstantTimeEq_Match(b *testing.B) {
	a := []byte("sk-deneb-abcdefghijklmnopqrstuvwx")
	bSlice := []byte("sk-deneb-abcdefghijklmnopqrstuvwx")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ConstantTimeEq(a, bSlice)
	}
}

func BenchmarkConstantTimeEq_Mismatch(b *testing.B) {
	a := []byte("sk-deneb-abcdefghijklmnopqrstuvwx")
	bSlice := []byte("sk-deneb-xxxxxxxxxxxxxxxxxxxxxxxx")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ConstantTimeEq(a, bSlice)
	}
}

// --- HTML sanitization (hot path: every outbound message) ---

func BenchmarkSanitizeHTML_Short(b *testing.B) {
	input := `Hello <b>world</b> & "friends"`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeHTML(input)
	}
}

func BenchmarkSanitizeHTML_LongMessage(b *testing.B) {
	input := strings.Repeat(`<p>안녕하세요 "세계" & 'friends'</p>`, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeHTML(input)
	}
}

// --- SSRF URL check (hot path: link extraction, external requests) ---

func BenchmarkIsSafeURL_Safe(b *testing.B) {
	url := "https://api.example.com/v1/data"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsSafeURL(url)
	}
}

func BenchmarkIsSafeURL_Blocked(b *testing.B) {
	url := "http://169.254.169.254/latest/meta-data/"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsSafeURL(url)
	}
}

// --- MIME detection (hot path: file uploads) ---

func BenchmarkDetectMIME_PNG(b *testing.B) {
	// PNG magic bytes
	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectMIME(data)
	}
}

func BenchmarkDetectMIME_JSON(b *testing.B) {
	data := []byte(`{"key":"value","nested":{"array":[1,2,3]}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectMIME(data)
	}
}

// --- Memory search: cosine similarity (hot path: vector search scoring) ---

func BenchmarkMemoryCosineSimilarity_768d(b *testing.B) {
	// 768-dimensional embeddings (common for embedding models)
	a := make([]float64, 768)
	bVec := make([]float64, 768)
	for i := range a {
		a[i] = rand.Float64()
		bVec[i] = rand.Float64()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MemoryCosineSimilarity(a, bVec)
	}
}

func BenchmarkMemoryCosineSimilarity_1536d(b *testing.B) {
	// 1536-dimensional (OpenAI ada-002 size)
	a := make([]float64, 1536)
	bVec := make([]float64, 1536)
	for i := range a {
		a[i] = rand.Float64()
		bVec[i] = rand.Float64()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MemoryCosineSimilarity(a, bVec)
	}
}

// --- BM25 score normalization (hot path: search ranking) ---

func BenchmarkMemoryBm25RankToScore(b *testing.B) {
	for i := 0; i < b.N; i++ {
		MemoryBm25RankToScore(float64(i % 100))
	}
}

// --- FTS query building (hot path: every memory search) ---

func BenchmarkMemoryBuildFtsQuery(b *testing.B) {
	query := "Deneb 게이트웨이 세션 관리 방법"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MemoryBuildFtsQuery(query)
	}
}

// --- Keyword extraction ---

func BenchmarkMemoryExtractKeywords(b *testing.B) {
	query := "how does the session manager handle concurrent access and timeout enforcement"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MemoryExtractKeywords(query)
	}
}

// --- Link extraction (hot path: per-message) ---

func BenchmarkExtractLinks(b *testing.B) {
	text := `Check out https://example.com/page and [this link](https://docs.deneb.ai/getting-started).
Also see http://api.example.com/v2/endpoint for more details.`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractLinks(text, 5)
	}
}

// --- Media token parsing (hot path: every LLM response) ---

func BenchmarkParseMediaTokens(b *testing.B) {
	text := "Here is the result.\nMEDIA: https://example.com/image.png\nAnd some more text."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = ParseMediaTokens(text)
	}
}

// --- Markdown to IR (hot path: streaming message parsing, cached) ---

func BenchmarkMarkdownToIR_Short(b *testing.B) {
	md := "# Hello\n\nSome **bold** and *italic* text with `inline code`."
	opts, _ := json.Marshal(map[string]any{})
	optsStr := string(opts)
	// Warm the cache.
	_, _ = MarkdownToIR(md, optsStr)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarkdownToIR(md, optsStr)
	}
}

func BenchmarkMarkdownToIR_Long(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("## Section ")
		sb.WriteString(strings.Repeat("x", 5))
		sb.WriteString("\n\nParagraph with **bold** and [link](https://example.com).\n\n")
		sb.WriteString("```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\n")
	}
	md := sb.String()
	opts, _ := json.Marshal(map[string]any{})
	optsStr := string(opts)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use unique markdown per iteration to bypass cache.
		_, _ = MarkdownToIR(md+strings.Repeat(" ", i%64), optsStr)
	}
}

// --- HTML to Markdown (used in web scraping) ---

func BenchmarkHtmlToMarkdown(b *testing.B) {
	html := "<h1>Title</h1><p>Hello <strong>world</strong>. Visit <a href=\"https://example.com\">here</a>.</p>"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = HtmlToMarkdown(html)
	}
}

// --- Hybrid result merge (hot path: search result ranking) ---

func BenchmarkMemoryMergeHybridResults(b *testing.B) {
	params := `{
		"vector_results": [
			{"id": "1", "score": 0.95},
			{"id": "2", "score": 0.85},
			{"id": "3", "score": 0.75}
		],
		"keyword_results": [
			{"id": "2", "rank": 1},
			{"id": "4", "rank": 2},
			{"id": "1", "rank": 3}
		],
		"vector_weight": 0.7,
		"keyword_weight": 0.3,
		"limit": 5
	}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MemoryMergeHybridResults(params)
	}
}
