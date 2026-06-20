// recall_semantic_leak_measure_test.go — REGRESSION GATE for the wiki
// semantic-only recall leak (end-to-end, through the chat recall pipeline).
//
// The recall_bench_test.go corpus runs WITHOUT an embedder (CI has no GPU /
// BGE-M3), so it never exercises store.Search's semantic blend — a documented
// blind spot that let the floorless semantic-only branch ship untested. These
// tests attach a deterministic, BGE-M3-band-calibrated mock embedder so the
// semantic-only path actually runs, then drive the REAL recall pipeline
// (recallSearchQueries → store.Search → buildRecallPreflight → formatRecall*)
// and assert that a weakly-similar off-topic wiki page is NOT injected into
// <recall-context> when no on-topic page exists.
//
// The leak being guarded is STRUCTURAL: searchSemantic keeps any cosine > 0
// (semantic.go) and mergeSearchResults' bonus/penalty cases both require inBM25,
// so before the search.go semanticOnlyFloor a page BM25 never matched kept its
// full raw cosine (measured: an off-topic page injected at score 0.6302). With
// the floor these tests pass; remove it (or set it to 0) and they fail.
//
// Why a mock and not real BGE-M3: this host has no reachable embedding server
// (:8001 not exposed on the tailnet — only the gateway :18789 is), so
// real-cosine measurement is impossible in CI. The mock reproduces the measured
// Korean cosine separation (irrelevant ~0.58-0.69, relevant ~0.77-0.86, per
// filestore/semindex.go:80-82). It is COLLISION-FREE: each text is exactly
// floor·e0 + topic·e_t, so a cross-topic pair sits at a fixed sub-floor cosine
// with no hash noise (an earlier idiosyncratic-hash variant could collide two
// cross-topic texts up into the relevant band and mask the gate).
//
// Run: go test ./internal/pipeline/chat/ -run RecallSemanticLeak -v
package chat

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// bandEmbedder is a deterministic, collision-free embedder whose cosine
// geometry reproduces the BGE-M3 Korean separation bands: a same-topic
// (query,doc) pair sits in the relevant band, a cross-topic pair at a fixed
// "irrelevant" cosine that is ABOVE 0 (so searchSemantic's >0 gate keeps it)
// but BELOW the wiki semantic-only floor (so mergeSearchResults must exclude
// it). Each text embeds to floorW·e0 + topicW·e_topic, L2-normalized:
//
//	cross-topic cosine = floorW² / (floorW²+topicW²)  (share only the floor axis)
//	same-topic  cosine = 1.0                          (share floor + topic axes)
//
// Fixing floorW² = crossCos and topicW² = 1-crossCos yields exactly crossCos
// cross-topic and 1.0 same-topic — a clean, deterministic separation with no
// idiosyncratic-hash axis to accidentally inflate a cross-topic pair.
type bandEmbedder struct {
	healthy  bool
	crossCos float64 // the exact cross-topic cosine produced; default 0.63 (irrelevant band)
}

func (b bandEmbedder) IsHealthy() bool { return b.healthy }

// bandTopicOf maps a text to a topic id by keyword; topic 0 ("no keyword")
// shares only the floor axis with everything.
func bandTopicOf(text string) int {
	lt := strings.ToLower(text)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(lt, strings.ToLower(s)) {
				return true
			}
		}
		return false
	}
	switch {
	case has("태양광", "모듈", "발전소", "인허가", "개발행위", "550w", "셀"):
		return 1 // solar
	case has("회계", "재무", "원가", "마진", "결산", "세무"):
		return 2 // finance
	case has("인사", "발령", "조직개편", "채용", "인선", "조직도"):
		return 3 // hr
	case has("gpu", "추론", "서버", "vllm", "게이트웨이"):
		return 4 // gpu
	case has("날씨", "기온", "맑", "더위"):
		return 5 // weather
	case has("화성", "이주", "이사", "정착"):
		return 6 // travel
	default:
		return 0 // generic floor-only
	}
}

func (b bandEmbedder) crossCosOrDefault() float64 {
	if b.crossCos > 0 {
		return b.crossCos
	}
	return 0.63 // middle of the measured Korean irrelevant band (0.58-0.69)
}

// Embed returns floorW·e0 + topicW·e_topic, L2-normalized (see type doc).
func (b bandEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	const dims = 8
	c := b.crossCosOrDefault()
	floorW := float32(math.Sqrt(c))
	topicW := float32(math.Sqrt(1 - c))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, dims)
		v[0] = floorW // shared Korean-language floor axis
		if tp := bandTopicOf(t); tp > 0 {
			v[tp] = topicW
		} else {
			v[dims-1] = topicW // dedicated generic axis: unit norm, sub-floor to every real topic
		}
		out[i] = v
	}
	return out, nil
}

func cosF32(a, b []float32) float64 {
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

// TestRecallSemanticLeak_BandCalibration proves the mock reproduces the measured
// Korean cosine bands (relevant strictly above irrelevant, irrelevant in the
// 0.58-0.69 band), so the leak gate that follows rests on realistic geometry.
func TestRecallSemanticLeak_BandCalibration(t *testing.T) {
	emb := bandEmbedder{healthy: true}
	ctx := context.Background()

	relevant := [][2]string{ // query + a doc on the SAME topic
		{"현대차 울산 태양광 모듈 납품 결제기한", "현대차 울산공장 태양광 550W 모듈 2000장 납품 결제 6월 말"},
		{"개발행위허가 토지 형질변경 진행상황", "태양광 발전소 개발행위허가 인허가 토지 형질변경 서류"},
		{"인사 발령 조직개편 명단", "인사 발령 조직개편 신규 인선 명단 공지"},
	}
	irrelevant := [][2]string{ // query + a doc on a DIFFERENT topic (leak candidates)
		{"오늘 날씨 어때 맑음", "태양광 발전소 개발행위허가 인허가 토지 형질변경 서류"},
		{"화성 이주 계획 정착 일정", "현대차 울산공장 태양광 550W 모듈 2000장 납품 결제 6월 말"},
		{"화성 이주 계획 정착 일정", "인사 발령 조직개편 신규 인선 명단 공지"},
		{"회계 결산 원가 마진 보고", "GPU 추론 서버 게이트웨이 vLLM 운영"},
	}

	measure := func(pairs [][2]string) (mn, mx float64) {
		mn, mx = 1.0, -1.0
		for _, p := range pairs {
			vs, _ := emb.Embed(ctx, []string{p[0], p[1]})
			c := cosF32(vs[0], vs[1])
			if c < mn {
				mn = c
			}
			if c > mx {
				mx = c
			}
			t.Logf("cos=%.4f  q=%q  doc=%q", c, p[0], truncForLog(p[1]))
		}
		return mn, mx
	}

	t.Log("--- RELEVANT (same-topic) pairs ---")
	rMin, rMax := measure(relevant)
	t.Log("--- IRRELEVANT (cross-topic) pairs ---")
	iMin, iMax := measure(irrelevant)

	if rMin <= iMax {
		t.Errorf("mock bands do not separate: relevant min %.3f <= irrelevant max %.3f", rMin, iMax)
	}
	// Irrelevant pairs must land in the measured Korean band; relevant clearly above.
	if !(iMax < 0.70 && iMin > 0.45) {
		t.Errorf("irrelevant band [%.3f,%.3f] outside the BGE-M3 envelope (~0.58-0.69)", iMin, iMax)
	}
	if rMax < 0.99 {
		t.Errorf("same-topic pairs should approach cosine 1.0, got max %.3f", rMax)
	}
}

// TestRecallSemanticLeak_NoOffTopicInjection is the core gate: a Korean recall
// query with an explicit cue, NO on-topic wiki page, and a corpus of only
// off-topic pages (cross-topic cosine in the irrelevant band). It drives the
// full recall pipeline and asserts NO off-topic wiki page reaches
// <recall-context> — and that the explicit cue instead yields the honest
// no-evidence notice. Without the search.go floor the off-topic pages are
// injected and this fails.
func TestRecallSemanticLeak_NoOffTopicInjection(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Four pages, NONE about the query topic (화성 이주 / travel), none sharing a
	// query token (so BM25 finds nothing → the floorless semantic-only path).
	pages := map[string]*wiki.Page{
		"거래/hyundai-ulsan.md": {Meta: wiki.Frontmatter{ID: "hyundai-ulsan", Title: "현대차 울산공장 모듈 납품", Category: "거래",
			Summary: "현대차 울산공장 태양광 550W 모듈 2000장 납품, 결제 6월 말", Tags: []string{"태양광", "모듈"}, Importance: 0.9},
			Body: "현대차 울산공장 태양광 550W 모듈 2000장 납품 결제. 셀 단가 협의."},
		"운영시스템/gateway.md": {Meta: wiki.Frontmatter{ID: "gateway", Title: "DGX 게이트웨이", Category: "운영시스템",
			Summary: "GPU 추론 게이트웨이 운영, vLLM 서버", Tags: []string{"gpu", "추론"}, Importance: 0.8},
			Body: "GPU 추론 서버 게이트웨이 vLLM 운영."},
		"인물/hr-notice.md": {Meta: wiki.Frontmatter{ID: "hr-notice", Title: "인사 발령 명단", Category: "인물",
			Summary: "인사 발령 조직개편 신규 인선 명단", Tags: []string{"인사", "조직개편"}, Importance: 0.7},
			Body: "인사 발령 조직개편 신규 인선 명단 공지."},
		"업무/finance.md": {Meta: wiki.Frontmatter{ID: "finance", Title: "분기 결산 보고", Category: "업무",
			Summary: "회계 결산 원가 마진 분기 보고", Tags: []string{"회계", "원가"}, Importance: 0.7},
			Body: "회계 결산 원가 마진 분기 보고 작성."},
	}
	for p, pg := range pages {
		if err := store.WritePage(p, pg); err != nil {
			t.Fatalf("WritePage %s: %v", p, err)
		}
	}
	// WarmSemanticIndex embeds all pages synchronously; the later store.Search
	// async re-embed is then a content-hash no-op, so search is deterministic.
	store.SetEmbedder(bandEmbedder{healthy: true})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("WarmSemanticIndex: %v", err)
	}

	// Explicit recall cue ("전에") + off-topic signal terms: recall runs, the
	// semantic branch runs (query >= 8 chars), BM25 finds nothing on-topic.
	const query = "전에 얘기했던 화성 이주 계획 정착 일정 어떻게 됐지?"

	// Stage 1-2: the merged ranking recall will consume must contain no off-topic page.
	queries := recallSearchQueries(query)
	for _, q := range queries {
		merged, err := store.Search(context.Background(), q, 3)
		if err != nil {
			t.Fatalf("store.Search(%q): %v", q, err)
		}
		for _, r := range merged {
			t.Errorf("LEAK at store.Search(%q): off-topic page %s admitted (score=%.4f)", q, r.Path, r.Score)
		}
	}

	// Stage 3-4: full recall preflight — no off-topic wiki row, honest notice.
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "client:main", Message: query},
		runDeps{wikiStore: store},
		nil,
	)
	t.Logf("--- buildRecallPreflight output ---\n%s", out)

	for p := range pages {
		if strings.Contains(out, p) {
			t.Errorf("LEAK: off-topic page %s injected into <recall-context>", p)
		}
	}
	if strings.Contains(out, "source=wiki") {
		t.Errorf("LEAK: a wiki row was injected for an off-topic-only corpus:\n%s", out)
	}
	// An explicit cue that found nothing real must surface the no-evidence notice,
	// not silently empty out (proves the gate excluded the leak rather than the
	// query never running).
	if strings.TrimSpace(out) != "" && !strings.Contains(out, "근거를 찾지 못") && !strings.Contains(out, "source=none") {
		t.Errorf("expected the no-evidence notice for an explicit cue with no on-topic page, got:\n%s", out)
	}
}

// TestRecallSemanticLeak_OnTopicStillSurfaces is the companion guard: the floor
// must exclude noise, not everything. With an ON-topic page present (same topic
// as the query → cosine 1.0) recall surfaces THAT page and still no off-topic
// page. This fails if the floor is set so high it drops genuine semantic hits.
func TestRecallSemanticLeak_OnTopicStillSurfaces(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	pages := map[string]*wiki.Page{
		// On-topic to the solar-deal query, but phrased so it shares NO query token
		// with "납품 결제기한 거래" verbatim except via meaning — exercises the
		// semantic branch, not BM25. (It does share 모듈/태양광, enough for BM25 too;
		// the point is it is genuinely relevant and must survive.)
		"거래/hyundai.md": {Meta: wiki.Frontmatter{ID: "h", Title: "현대차 울산 태양광 모듈 납품", Category: "거래",
			Summary: "태양광 550W 모듈 2000장 납품 결제 6월 말"}, Body: "태양광 모듈 셀 납품 결제."},
		"운영시스템/gw.md": {Meta: wiki.Frontmatter{ID: "g", Title: "GPU 게이트웨이", Category: "운영시스템",
			Summary: "GPU 추론 서버 vLLM"}, Body: "GPU 추론 서버."},
		"인물/hr.md": {Meta: wiki.Frontmatter{ID: "p", Title: "인사 발령 명단", Category: "인물",
			Summary: "인사 발령 조직개편 인선"}, Body: "인사 발령 조직개편."},
	}
	for p, pg := range pages {
		if err := store.WritePage(p, pg); err != nil {
			t.Fatalf("WritePage %s: %v", p, err)
		}
	}
	store.SetEmbedder(bandEmbedder{healthy: true})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("WarmSemanticIndex: %v", err)
	}

	// Solar-deal query: on-topic to 거래/hyundai.md (cosine 1.0), off-topic to the
	// rest (cross-topic cosine ~0.63, below the floor).
	const query = "태양광 모듈 납품 결제기한 거래 건"
	merged, err := store.Search(context.Background(), query, 5)
	if err != nil {
		t.Fatalf("store.Search: %v", err)
	}
	var hasOnTopic bool
	for _, r := range merged {
		if r.Path == "거래/hyundai.md" {
			hasOnTopic = true
			continue
		}
		t.Errorf("LEAK: off-topic page %s admitted alongside the on-topic page (score=%.4f)", r.Path, r.Score)
	}
	if !hasOnTopic {
		t.Errorf("on-topic solar page was excluded — floor is too aggressive for genuine hits")
	}
}

func truncForLog(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:40]) + "…"
}
