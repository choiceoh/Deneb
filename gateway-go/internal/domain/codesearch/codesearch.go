// Package codesearch is the semantic (concept) code search layer over the
// CodeGraph index — the one axis where symbol/lexical search trails frontier
// tooling. It embeds every non-test function/method/struct (qualified name +
// signature + docstring + a source excerpt) with the local Nemotron sidecar,
// and answers natural-language queries by fusing dense cosine hits with
// CodeGraph's FTS hits via RRF — the same fusion pattern as wiki recall.
//
// Storage is a sidecar pair under .codegraph/ (NOT in git):
//
//	semantic-code.json — entry metadata (id, symbol, file:line, updated_at)
//	semantic-code.vec  — raw little-endian float32 vectors, entry order
//
// Incremental: entries re-embed only when the CodeGraph node's updated_at
// moved; deleted nodes drop out. A model/dimension change forces a full build.
package codesearch

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

// Embedder matches internal/ai/embedding.Client. Role-aware implementations use
// the query role through embedindex; symmetric embedders retain plain Embed.
type Embedder interface {
	embedindex.TextEmbedder
}

// Entry is one embedded code unit (vector lives in the .vec file at the same
// ordinal position).
type Entry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Language  string `json:"language"`
	Qualified string `json:"qualified"`
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Meta is the JSON sidecar header.
type Meta struct {
	Model   string  `json:"model"`
	Dim     int     `json:"dim"`
	Entries []Entry `json:"entries"`
}

const (
	metaName = "semantic-code.json"
	vecName  = "semantic-code.vec"
	// excerptLines bounds the source excerpt per unit — enough body for the
	// embedding to smell the concept without drowning in boilerplate.
	excerptLines = 30
	// minScore mirrors filestore's measured Nemotron floor: cosine below this
	// is noise on this model's scale.
	minScore = 0.05
)

func metaPath(codegraphDir string) string { return filepath.Join(codegraphDir, metaName) }
func vecPath(codegraphDir string) string  { return filepath.Join(codegraphDir, vecName) }

// LoadMeta reads the index header; ok=false when absent/corrupt (build needed).
func LoadMeta(dir string) (Meta, bool) {
	var m Meta
	b, err := os.ReadFile(metaPath(dir))
	if err != nil || json.Unmarshal(b, &m) != nil || m.Dim <= 0 {
		return Meta{}, false
	}
	return m, true
}

func saveIndex(dir string, m Meta, vecs [][]float32) error {
	buf := make([]byte, 0, len(vecs)*m.Dim*4)
	tmp := make([]byte, 4)
	for _, v := range vecs {
		if len(v) != m.Dim {
			return fmt.Errorf("vector dim %d != %d", len(v), m.Dim)
		}
		for _, f := range v {
			binary.LittleEndian.PutUint32(tmp, math.Float32bits(f))
			buf = append(buf, tmp...)
		}
	}
	if err := os.WriteFile(vecPath(dir)+".tmp", buf, 0o644); err != nil {
		return err
	}
	mb, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(metaPath(dir)+".tmp", mb, 0o644); err != nil {
		return err
	}
	if err := os.Rename(vecPath(dir)+".tmp", vecPath(dir)); err != nil {
		return err
	}
	return os.Rename(metaPath(dir)+".tmp", metaPath(dir))
}

// loadVectors reads the raw vector file back as per-entry slices.
func loadVectors(dir string, m Meta) ([][]float32, error) {
	b, err := os.ReadFile(vecPath(dir))
	if err != nil {
		return nil, err
	}
	want := len(m.Entries) * m.Dim * 4
	if len(b) != want {
		return nil, fmt.Errorf("vec file %d bytes, want %d (meta drift — rebuild)", len(b), want)
	}
	out := make([][]float32, len(m.Entries))
	for i := range out {
		v := make([]float32, m.Dim)
		base := i * m.Dim * 4
		for j := 0; j < m.Dim; j++ {
			v[j] = math.Float32frombits(binary.LittleEndian.Uint32(b[base+j*4:]))
		}
		out[i] = v
	}
	return out, nil
}

type node struct {
	Entry
	Signature string
	Docstring string
}

// loadNodes pulls embeddable units from the CodeGraph DB, excluding tests.
// queryJSON shells out to the sqlite3 CLI (-json) so the gateway module
// needs no cgo/sqlite dependency — the CodeGraph DB is a read-only sidecar.
// Rows decode straight into T so column/type mismatches fail loudly instead
// of silently zeroing (no map[string]any laundering).
func queryJSON[T any](ctx context.Context, dbPath, query string) ([]T, error) {
	// #nosec G204 -- fixed binary; dbPath is the local .codegraph sidecar and
	// query is composed from constant SQL with single quotes stripped from user terms.
	out, err := exec.CommandContext(ctx, "sqlite3", "-json", "file:"+dbPath+"?mode=ro", query).Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}
	var rows []T
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("sqlite3 json: %w", err)
	}
	return rows, nil
}

// nodeRow mirrors the loadNodes SELECT column aliases one-to-one.
type nodeRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Language  string `json:"language"`
	Qualified string `json:"q"`
	File      string `json:"f"`
	StartLine int    `json:"s"`
	EndLine   int    `json:"e"`
	Signature string `json:"sig"`
	Docstring string `json:"doc"`
	UpdatedAt int64  `json:"u"`
}

func loadNodes(ctx context.Context, dbPath string) ([]node, error) {
	rows, err := queryJSON[nodeRow](ctx, dbPath,
		`SELECT id, kind, language, qualified_name AS q, file_path AS f, start_line AS s, end_line AS e,
		        COALESCE(signature,'') AS sig, COALESCE(docstring,'') AS doc, COALESCE(updated_at,0) AS u
		 FROM nodes
		 WHERE kind IN ('function','method','struct','class')
		   AND file_path NOT LIKE '%_test.go'
		   AND file_path NOT LIKE '%.test.%'
		   AND file_path NOT LIKE '%Test.kt'
		   AND file_path NOT LIKE '%node_modules%'`)
	if err != nil {
		return nil, err
	}
	out := make([]node, 0, len(rows))
	for _, r := range rows {
		out = append(out, node{
			Entry: Entry{
				ID:        r.ID,
				Kind:      r.Kind,
				Language:  r.Language,
				Qualified: r.Qualified,
				File:      r.File,
				StartLine: r.StartLine,
				EndLine:   r.EndLine,
				UpdatedAt: r.UpdatedAt,
			},
			Signature: r.Signature,
			Docstring: r.Docstring,
		})
	}
	return out, nil
}

// embedText renders one unit into the text the model sees.
func embedText(repo string, n node) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s %s\n%s\n", n.Language, n.Kind, n.Qualified, n.File)
	if n.Signature != "" {
		sb.WriteString(n.Signature + "\n")
	}
	if n.Docstring != "" {
		sb.WriteString(n.Docstring + "\n")
	}
	if body, err := os.ReadFile(filepath.Join(repo, n.File)); err == nil {
		lines := strings.Split(string(body), "\n")
		lo := n.StartLine - 1
		hi := min(n.EndLine, lo+excerptLines)
		if lo >= 0 && lo < len(lines) {
			hi = min(hi, len(lines))
			sb.WriteString(strings.Join(lines[lo:hi], "\n"))
		}
	}
	return sb.String()
}

// BuildIndex embeds new/changed units and rewrites the sidecar pair.
// Returns (embedded, reused, removed).
func BuildIndex(ctx context.Context, repo, dir string, emb Embedder, model string, dim int, full bool, progress func(string)) (int, int, int, error) {
	nodes, err := loadNodes(ctx, filepath.Join(dir, "codegraph.db"))
	if err != nil {
		return 0, 0, 0, err
	}
	prev, ok := LoadMeta(dir)
	var prevVecs [][]float32
	reuse := map[string]int{}
	if ok && !full && prev.Model == model && prev.Dim == dim {
		if prevVecs, err = loadVectors(dir, prev); err == nil {
			for i, e := range prev.Entries {
				reuse[e.ID+"@"+fmt.Sprint(e.UpdatedAt)] = i
			}
		} else {
			prevVecs = nil
		}
	}

	meta := Meta{Model: model, Dim: dim}
	var vecs [][]float32
	var toEmbed []node
	var embedIdx []int
	reused := 0
	for _, n := range nodes {
		meta.Entries = append(meta.Entries, n.Entry)
		if i, hit := reuse[n.ID+"@"+fmt.Sprint(n.UpdatedAt)]; hit && prevVecs != nil {
			vecs = append(vecs, prevVecs[i])
			reused++
			continue
		}
		vecs = append(vecs, nil)
		toEmbed = append(toEmbed, n)
		embedIdx = append(embedIdx, len(vecs)-1)
	}

	const batch = 128
	for off := 0; off < len(toEmbed); off += batch {
		end := min(off+batch, len(toEmbed))
		texts := make([]string, 0, end-off)
		for _, n := range toEmbed[off:end] {
			texts = append(texts, embedText(repo, n))
		}
		got, err := emb.Embed(ctx, texts)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("embed batch @%d: %w", off, err)
		}
		if len(got) != len(texts) {
			return 0, 0, 0, fmt.Errorf("embed batch @%d: %d vectors for %d texts", off, len(got), len(texts))
		}
		for i, v := range got {
			vecs[embedIdx[off+i]] = v
		}
		if progress != nil {
			progress(fmt.Sprintf("embedded %d/%d (reused %d)", end, len(toEmbed), reused))
		}
	}
	removed := 0
	if ok {
		removed = max(0, len(prev.Entries)-reused)
	}
	return len(toEmbed), reused, removed, saveIndex(dir, meta, vecs)
}

// Hit is one search result after fusion.
type Hit struct {
	Entry
	Score  float64 // RRF score
	Cosine float64 // dense similarity (0 when FTS-only)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Reranker mirrors wiki.Reranker: an optional cross-encoder sidecar
// (XProvence :8004 in production). Search stays fully functional without it
// and falls back unchanged on any error.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
	Identity() string
}

const (
	rerankCandidates   = 10
	rerankDocChars     = 600
	rerankTimeoutShort = 800 * time.Millisecond
)

// rerankHits re-orders the head of the fused list by cross-encoder relevance.
// Documents carry the symbol header plus a source-head excerpt — same shape
// the wiki reranker validated offline (600 chars, top-10, one fast batch).
func rerankHits(ctx context.Context, repo string, rr Reranker, query string, hits []Hit) []Hit {
	n := min(rerankCandidates, len(hits))
	if rr == nil || n < 2 {
		return hits
	}
	docs := make([]string, n)
	for i, h := range hits[:n] {
		docs[i] = h.Qualified + "\n" + h.File + "\n" + sourceHead(repo, h.Entry)
		if len(docs[i]) > rerankDocChars {
			docs[i] = docs[i][:rerankDocChars]
		}
	}
	rctx, cancel := context.WithTimeout(ctx, rerankTimeoutShort)
	defer cancel()
	scores, err := rr.Rerank(rctx, query, docs)
	if err != nil || len(scores) != n {
		return hits
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	out := make([]Hit, 0, len(hits))
	for _, i := range idx {
		out = append(out, hits[i])
	}
	return append(out, hits[n:]...)
}

// sourceHead returns the first lines of the symbol's source, cheap file read.
func sourceHead(repo string, e Entry) string {
	data, err := os.ReadFile(filepath.Join(repo, e.File))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if e.StartLine-1 < 0 || e.StartLine-1 >= len(lines) {
		return ""
	}
	end := min(e.StartLine-1+12, len(lines))
	return strings.Join(lines[e.StartLine-1:end], "\n")
}

// Search fuses dense top-K with CodeGraph FTS top-K by reciprocal rank (k=60),
// mirroring the wiki-recall fusion that measurably beat either alone.
func Search(ctx context.Context, dir string, emb Embedder, query string, topK int) ([]Hit, error) {
	meta, ok := LoadMeta(dir)
	if !ok {
		return nil, fmt.Errorf("semantic index missing — run `codesearch index` first")
	}
	vecs, err := loadVectors(dir, meta)
	if err != nil {
		return nil, err
	}
	query = expandQuery(query)
	qv, err := embedindex.EmbedQueries(ctx, emb, []string{query})
	if err != nil || len(qv) != 1 {
		return nil, fmt.Errorf("query embed failed: %w", err)
	}

	type scored struct {
		idx int
		cos float64
	}
	dense := make([]scored, 0, len(meta.Entries))
	for i := range meta.Entries {
		if c := cosine(qv[0], vecs[i]); c >= minScore {
			dense = append(dense, scored{i, c})
		}
	}
	sort.Slice(dense, func(i, j int) bool { return dense[i].cos > dense[j].cos })
	if len(dense) > 20 {
		dense = dense[:20]
	}

	// Lexical arm: CodeGraph FTS over the same node universe.
	ftsRank := map[string]int{}
	q := strings.ReplaceAll(ftsQuery(query), "'", "")
	type idRow struct {
		ID string `json:"id"`
	}
	if rows, err := queryJSON[idRow](ctx, filepath.Join(dir, "codegraph.db"),
		"SELECT id FROM nodes_fts WHERE nodes_fts MATCH '"+q+"' ORDER BY rank LIMIT 20"); err == nil {
		for r, row := range rows {
			ftsRank[row.ID] = r
		}
	}

	const rrfK = 60.0
	byID := map[string]*Hit{}
	entryByID := map[string]int{}
	for i, e := range meta.Entries {
		entryByID[e.ID] = i
	}
	for r, s := range dense {
		e := meta.Entries[s.idx]
		byID[e.ID] = &Hit{Entry: e, Score: 1 / (rrfK + float64(r) + 1), Cosine: s.cos}
	}
	for id, r := range ftsRank {
		i, known := entryByID[id]
		if !known {
			continue // FTS hit outside the embeddable universe (imports, files…)
		}
		if h, dup := byID[id]; dup {
			h.Score += 1 / (rrfK + float64(r) + 1)
		} else {
			byID[id] = &Hit{Entry: meta.Entries[i], Score: 1 / (rrfK + float64(r) + 1)}
		}
	}
	hint := kindHint(query)
	out := make([]Hit, 0, len(byID))
	for _, h := range byID {
		if hint != "" && h.Kind == hint {
			h.Score *= 1.3
		}
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

// koEnSynonyms bridges the Korean-query → English-code semantic gap for this
// repo's recurring domain terms. Expansion is applied to both search arms:
// the embed text gains English anchors, FTS gains lexical hits it could
// never get from Hangul tokens.
var koEnSynonyms = map[string][]string{
	"전사":   {"transcribe", "transcription", "asr"},
	"화자분리": {"diarize", "diarization", "speaker"},
	"음성":   {"speech", "audio", "voice"},
	"결재":   {"approval", "groupware"},
	"승인":   {"approve", "accept"},
	"반려":   {"reject"},
	"회상":   {"recall"},
	"임베딩":  {"embedding"},
	"첨부":   {"attachment"},
	"메일":   {"mail", "gmail"},
	"위키":   {"wiki"},
	"세션":   {"session"},
	"재시도":  {"retry"},
	"백오프":  {"backoff"},
	"캐시":   {"cache"},
	"컴팩션":  {"compaction", "compact"},
	"프롬프트": {"prompt"},
	"스킬":   {"skill"},
	"진화":   {"evolve", "evolution"},
	"검색":   {"search", "query"},
	"융합":   {"fusion", "fuse", "rrf"},
	"카드":   {"card"},
	"작업피드": {"workfeed"},
	"피드":   {"feed", "workfeed"},
	"알림":   {"notification", "notify"},
	"배포":   {"deploy"},
	"동기화":  {"sync"},
	"암호화":  {"encrypt", "crypto"},
	"인증":   {"auth", "token"},
	"일정":   {"calendar", "schedule"},
	"거래처":  {"client", "account", "company"},
	"전화번호": {"phone"},
	"주소록":  {"contact"},
	"조직도":  {"org", "organization"},
	"크론":   {"cron"},
	"도구":   {"tool"},
	"모델":   {"model", "llm"},
	"스트리밍": {"stream", "sse"},
	"업로드":  {"upload"},
	"다운로드": {"download", "fetch"},
	"텔레그램": {"telegram"},
	"슬랙":   {"slack"},
	"브라우저": {"browser"},
	"본문":   {"content", "article", "readability", "body"},
	"유튜브":  {"youtube"},
	"자막":   {"transcript", "caption", "subtitle"},
	"파싱":   {"parse", "parser"},
	"렌더링":  {"render"},
	"스크린샷": {"screenshot", "capture"},
	"북마크":  {"bookmark"},
	"스크롤":  {"scroll"},
	"로그인":  {"login"},
	"백업":   {"backup"},
	"필터":   {"filter"},
	"라우팅":  {"route", "routing"},
	"프록시":  {"proxy"},
	"녹음":   {"recording", "audio"},
	"회의록":  {"meeting", "plaud"},
	"요약":   {"summary", "summarize"},
	"번역":   {"translate"},
	"날씨":   {"weather"},
	"주가":   {"stock", "market"},
	"환율":   {"exchange", "currency", "fx"},
}

// kindHint reads an explicit symbol-kind ask out of the query ("~ 구조체",
// "handler method", …) so results of that kind outrank same-relevance noise.
// Empty string = no hint.
func kindHint(q string) string {
	lower := strings.ToLower(q)
	for kind, words := range map[string][]string{
		"struct":   {"구조체", "struct"},
		"class":    {"클래스", "class"},
		"method":   {"메서드", "메소드", "method"},
		"function": {"함수", "function", "func "},
	} {
		for _, w := range words {
			if strings.Contains(lower, w) {
				return kind
			}
		}
	}
	return ""
}

// expandQuery appends English domain anchors for any Korean term present.
func expandQuery(q string) string {
	var extra []string
	seen := map[string]bool{}
	for ko, ens := range koEnSynonyms {
		if strings.Contains(q, ko) {
			for _, en := range ens {
				if !seen[en] && !strings.Contains(q, en) {
					seen[en] = true
					extra = append(extra, en)
				}
			}
		}
	}
	if len(extra) == 0 {
		return q
	}
	sort.Strings(extra)
	return q + " " + strings.Join(extra, " ")
}

// SearchRanked is Search plus an optional cross-encoder pass over the fused
// head. repo is the checkout root (source excerpts feed the rerank docs).
func SearchRanked(ctx context.Context, repo, dir string, emb Embedder, rr Reranker, query string, topK int) ([]Hit, error) {
	// Over-fetch so the reranker sees the full candidate head even for small topK.
	hits, err := Search(ctx, dir, emb, query, max(topK, rerankCandidates))
	if err != nil {
		return nil, err
	}
	hits = rerankHits(ctx, repo, rr, query, hits)
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// ftsQuery quotes each term so FTS5 treats natural language safely.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	for i, f := range fields {
		fields[i] = `"` + strings.ReplaceAll(f, `"`, "") + `"`
	}
	return strings.Join(fields, " OR ")
}
