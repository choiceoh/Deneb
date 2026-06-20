package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// liveEmbedder talks to a real BGE-M3 server (the same /embed wire shape as
// internal/ai/embedding.Client) so the hybrid-vs-floor comparison runs on
// genuine Korean cosines, not a hand-placed fake. Gated by DENEB_OCR-style env:
//
//	DENEB_EMBED_LIVE=1 DENEB_EMBED_URL=http://127.0.0.1:8001 \
//	  go test -run TestHybridSearch_Live -v ./internal/domain/filestore/
//
// On the DGX host, tunnel srv4's loopback BGE-M3 first:
//
//	ssh -N -L 8001:127.0.0.1:8001 srv4 &
type liveEmbedder struct {
	url string
	hc  *http.Client
}

func (l *liveEmbedder) IsHealthy() bool { return true }

func (l *liveEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string][]string{"texts": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed HTTP %d", resp.StatusCode)
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// TestHybridSearch_Live measures floor-only Search vs hybrid HybridSearch on a
// real BGE-M3 server, over a small Korean office-doc corpus. It is a measurement
// harness (not a CI assertion): it prints per-query keep/drop for both methods
// and only hard-fails on the load-bearing invariants — exact filename queries
// must survive hybrid, and clearly-irrelevant queries must stay empty in both.
func TestHybridSearch_Live(t *testing.T) {
	if os.Getenv("DENEB_EMBED_LIVE") != "1" {
		t.Skip("set DENEB_EMBED_LIVE=1 (and DENEB_EMBED_URL) to run the live BGE-M3 comparison")
	}
	url := os.Getenv("DENEB_EMBED_URL")
	if url == "" {
		url = "http://127.0.0.1:8001"
	}
	embed := &liveEmbedder{url: url, hc: &http.Client{Timeout: 60 * time.Second}}
	// Fail fast if the server isn't actually answering embeds.
	if _, err := embed.Embed(context.Background(), []string{"헬스 체크"}); err != nil {
		t.Fatalf("BGE-M3 not reachable at %s: %v", url, err)
	}

	ctx := context.Background()
	store := newTestStore(t)
	// A realistic Korean office corpus: each file's body is a few sentences of the
	// kind of mixed Korean/number/term text BGE-M3 packs into its high cosine band.
	corpus := map[string]string{
		"/인허가/개발행위허가 신청서.txt":      "토지 형질변경을 위한 개발행위허가 신청 서류입니다. 부지 면적과 용도지역, 인허가 절차 일정과 담당 부서 연락처를 포함합니다.",
		"/계약/탑솔라 모듈 공급계약서.txt":     "태양광 모듈 공급 계약 조건입니다. 단가, 납기 일정, 위약금 조항과 하자보수 보증 기간을 규정합니다.",
		"/인사/2025 상반기 인사발령 명단.txt": "2025년 상반기 인사발령 명단입니다. 부서 이동과 승진 대상자, 발령 일자를 정리한 표를 포함합니다.",
		"/재무/3분기 매출 보고서.txt":       "3분기 매출 실적 보고입니다. 사업부별 매출액과 전년 동기 대비 성장률, 영업이익률을 분석합니다.",
		"/회의/주간 점심 메뉴표.txt":        "이번 주 구내식당 점심 메뉴표입니다. 요일별 메인 메뉴와 후식, 커피 음료 목록을 안내합니다.",
		"/물류/케이블 재고 현황.txt":        "전선 케이블 품목별 재고 현황입니다. 규격별 수량과 입출고 내역, 안전 재고 수준을 표로 정리했습니다.",
	}
	for p, b := range corpus {
		mustPut(t, store, p, b)
	}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// relevant: should be kept by BOTH (meaning match).
	relevant := []struct{ query, wantPath string }{
		{"토지 형질변경 인허가 절차", "/인허가/개발행위허가 신청서.txt"},
		{"모듈 납품 단가와 납기 위약금", "/계약/탑솔라 모듈 공급계약서.txt"},
		{"승진 대상자 발령 일자", "/인사/2025 상반기 인사발령 명단.txt"},
	}
	// exactName: the query is (mostly) the file's NAME words but phrased so the
	// body cosine may dip — the case hybrid is meant to rescue.
	exactName := []struct{ query, wantPath string }{
		{"개발행위허가 신청서 어디 있어", "/인허가/개발행위허가 신청서.txt"},
		{"탑솔라 모듈 공급계약서 보여줘", "/계약/탑솔라 모듈 공급계약서.txt"},
		{"케이블 재고 현황 파일", "/물류/케이블 재고 현황.txt"},
	}
	// irrelevant: should be EMPTY in both (Korean noise band, no lexical overlap).
	irrelevant := []string{
		"오늘 날씨 어때",
		"주말에 볼 만한 영화 추천",
		"이번 휴가 항공권 예약",
	}

	keep := func(hits []ScoredEntry, path string) (bool, float64) {
		for _, h := range hits {
			if h.Entry.PathDisplay == path {
				return true, h.Score
			}
		}
		return false, 0
	}

	t.Logf("=== relevant (both should keep) ===")
	floorRelKept, hybRelKept := 0, 0
	for _, c := range relevant {
		sem, _ := idx.Search(ctx, c.query, 10, embed)
		hyb, _ := idx.HybridSearch(ctx, c.query, 10, embed, plainText)
		fk, fs := keep(sem, c.wantPath)
		hk, hs := keep(hyb, c.wantPath)
		if fk {
			floorRelKept++
		}
		if hk {
			hybRelKept++
		}
		t.Logf("  %-28q floor=%v(%.3f) hybrid=%v(%.3f)", c.query, fk, fs, hk, hs)
	}

	t.Logf("=== exactName (hybrid should keep; floor may drop) ===")
	floorNameKept, hybNameKept := 0, 0
	for _, c := range exactName {
		sem, _ := idx.Search(ctx, c.query, 10, embed)
		hyb, _ := idx.HybridSearch(ctx, c.query, 10, embed, plainText)
		fk, fs := keep(sem, c.wantPath)
		hk, hs := keep(hyb, c.wantPath)
		if fk {
			floorNameKept++
		}
		if hk {
			hybNameKept++
		}
		t.Logf("  %-28q floor=%v(%.3f) hybrid=%v(%.3f)", c.query, fk, fs, hk, hs)
		if !hk {
			t.Errorf("HYBRID DROPPED exact-name query %q (want %s)", c.query, c.wantPath)
		}
	}

	t.Logf("=== irrelevant (both should be empty) ===")
	floorIrrKept, hybIrrKept := 0, 0
	for _, q := range irrelevant {
		sem, _ := idx.Search(ctx, q, 10, embed)
		hyb, _ := idx.HybridSearch(ctx, q, 10, embed, plainText)
		floorIrrKept += len(sem)
		hybIrrKept += len(hyb)
		t.Logf("  %-28q floor=%d hits, hybrid=%d hits", q, len(sem), len(hyb))
		if len(hyb) != 0 {
			paths := make([]string, 0, len(hyb))
			for _, h := range hyb {
				paths = append(paths, fmt.Sprintf("%s(%.3f)", h.Entry.PathDisplay, h.Score))
			}
			t.Errorf("HYBRID returned %d hits for irrelevant query %q: %v", len(hyb), q, paths)
		}
	}

	t.Logf("=== SUMMARY (kept counts) ===")
	t.Logf("  relevant   : floor %d/%d  hybrid %d/%d", floorRelKept, len(relevant), hybRelKept, len(relevant))
	t.Logf("  exactName  : floor %d/%d  hybrid %d/%d  <-- hybrid gain", floorNameKept, len(exactName), hybNameKept, len(exactName))
	t.Logf("  irrelevant : floor %d total hits  hybrid %d total hits", floorIrrKept, hybIrrKept)
}

// TestHybridSearch_LiveSubfloorNameMatch is the adversarial case that the topical
// corpus above can't produce: a file whose NAME contains the query terms but
// whose BODY is generic boilerplate UNRELATED to those terms, so the real BGE-M3
// body cosine lands BELOW the 0.73 floor. The floor-only Search drops it; hybrid
// must rescue it on the exact-name signal. This is the load-bearing demonstration
// of the hybrid gain on real vectors.
func TestHybridSearch_LiveSubfloorNameMatch(t *testing.T) {
	if os.Getenv("DENEB_EMBED_LIVE") != "1" {
		t.Skip("set DENEB_EMBED_LIVE=1 (and DENEB_EMBED_URL) to run the live BGE-M3 comparison")
	}
	url := os.Getenv("DENEB_EMBED_URL")
	if url == "" {
		url = "http://127.0.0.1:8001"
	}
	embed := &liveEmbedder{url: url, hc: &http.Client{Timeout: 60 * time.Second}}
	if _, err := embed.Embed(context.Background(), []string{"헬스 체크"}); err != nil {
		t.Fatalf("BGE-M3 not reachable at %s: %v", url, err)
	}

	ctx := context.Background()
	store := newTestStore(t)
	// The KEY file: its name says "탑솔라 정산 합의서" but its body is a generic
	// cover note that never discusses 정산/합의 — a real document whose content
	// drifted from its title (a scan cover page, a placeholder, etc.).
	mustPut(t, store, "/계약/탑솔라 정산 합의서.txt",
		"수신: 관련 부서. 첨부 문서를 확인 바랍니다. 기타 문의 사항은 담당자에게 연락 주시기 바랍니다. 감사합니다.")
	// A few topical distractors so the corpus is realistic and BM25 IDF is sane.
	mustPut(t, store, "/회의/주간 점심 메뉴표.txt",
		"이번 주 구내식당 점심 메뉴표입니다. 요일별 메인 메뉴와 후식, 커피 음료 목록을 안내합니다.")
	mustPut(t, store, "/재무/3분기 매출 보고서.txt",
		"3분기 매출 실적 보고입니다. 사업부별 매출액과 전년 동기 대비 성장률을 분석합니다.")

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	const target = "/계약/탑솔라 정산 합의서.txt"
	const query = "탑솔라 정산 합의서" // matches the NAME; body is unrelated boilerplate

	sem, _ := idx.Search(ctx, query, 10, embed)
	hyb, _ := idx.HybridSearch(ctx, query, 10, embed, plainText)

	floorKept, floorScore := false, 0.0
	for _, h := range sem {
		if h.Entry.PathDisplay == target {
			floorKept, floorScore = true, h.Score
		}
	}
	hybKept, hybScore := false, 0.0
	for _, h := range hyb {
		if h.Entry.PathDisplay == target {
			hybKept, hybScore = true, h.Score
		}
	}
	t.Logf("subfloor name-match: target body cosine=%.3f (floor=%.2f)", hybScore, minSemanticScore)
	t.Logf("  floor-only Search kept=%v (score %.3f)", floorKept, floorScore)
	t.Logf("  hybrid       Search kept=%v (score %.3f)", hybKept, hybScore)

	// The whole point: hybrid must keep the exact-name file regardless of where
	// the body cosine lands.
	if !hybKept {
		t.Fatalf("hybrid dropped the exact-name file %q (query=%q) — the gain failed", target, query)
	}
	if floorKept && hybKept {
		t.Logf("  NOTE: body cosine %.3f cleared the floor too — this corpus didn't force a sub-floor case", hybScore)
	}
	if !floorKept && hybKept {
		t.Logf("  ✓ DEMONSTRATED GAIN: floor-only dropped it (cosine %.3f < %.2f), hybrid rescued it via name match",
			hybScore, minSemanticScore)
	}
}
