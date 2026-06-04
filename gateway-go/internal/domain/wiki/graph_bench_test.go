package wiki

// graph_bench_test.go — an offline benchmark for the similarity scorer.
//
// "Related" is graded, not binary: "비금도 케이블 발주" and "비금도 모듈 소손" share a
// site but are different work items — neither cleanly related nor spurious. So
// the judge rates each pair 0..3 (0=unrelated, 1=weak: shares only a client/
// owner/region, 2=same site different work, 3=same project or direct follow-up)
// and the metric is whether the scorer RANKS by that grade, not whether it
// crosses a binary line.
//
// Ground truth: the local step3p7 (a GLM-5.1-class model) reasoning over each
// pair, made to emit GRADE + a one-line REASON so every label is auditable
// (step3p7 reasons well; it just needs enough tokens to reach the marker after
// its chain of thought). Labels cache to ~/.deneb/wiki-graph/bench-grades.json.
//
//	DENEB_WIKI_BENCH=1 go test -run TestGraphBench -v -timeout 90m ./internal/domain/wiki/

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	benchJudgeURLDefault   = "http://100.125.220.117:8000/v1/chat/completions"
	benchJudgeModelDefault = "step3p7"
	benchDefaultSeeds      = "비금도,대한전선,현대차,진도,영광,남도에코,트리나솔라,당진,고흥,LG화학"
	// benchCandFloor pulls scorer-connected pages (not just shared-tag) into the
	// judged candidate set, so false positives are measured, not hidden.
	benchCandFloor = 0.2
)

// benchLabel is one judged pair: the grade plus the judge's one-line reason,
// kept so the ground truth stays auditable.
type benchLabel struct {
	Grade  int    `json:"grade"`
	Reason string `json:"reason"`
}

func TestGraphBench(t *testing.T) {
	if os.Getenv("DENEB_WIKI_BENCH") == "" {
		t.Skip("set DENEB_WIKI_BENCH=1 to run the similarity benchmark against ~/.deneb/wiki")
	}
	home, _ := os.UserHomeDir()
	store, err := NewStore(filepath.Join(home, ".deneb", "wiki"), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	store.mu.RLock()
	entries := make(map[string]IndexEntry, len(store.index.Entries))
	for p, e := range store.index.Entries {
		entries[p] = e
	}
	store.mu.RUnlock()

	seeds := strings.Split(benchEnv("DENEB_WIKI_BENCH_SEEDS", benchDefaultSeeds), ",")
	judgeURL := benchEnv("DENEB_WIKI_BENCH_URL", benchJudgeURLDefault)
	judgeModel := benchEnv("DENEB_WIKI_BENCH_MODEL", benchJudgeModelDefault)
	cachePath := filepath.Join(home, ".deneb", "wiki-graph", "bench-grades.json")
	labels := benchLoadLabels(cachePath)

	type sample struct {
		score float64
		grade int
	}
	var samples []sample
	newLabels := 0

	for _, q := range seeds {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		seedPath := store.resolveSeedPath(context.Background(), q)
		if seedPath == "" {
			t.Logf("seed %q: unresolved, skipping", q)
			continue
		}
		seed := entries[seedPath]
		seedCtx := store.benchPageCtx(seedPath, seed)
		scores := store.graphScore(seedPath, seed, entries)

		// Candidates = pages sharing a tag OR connected by the scorer (score ≥
		// floor). The scorer side is what makes false positives visible: a hub
		// page wrongly linked via cross-ref gets judged (→ grade 0) and drags the
		// metric down, instead of hiding outside a tag-only candidate set.
		candSet := map[string]struct{}{}
		for _, cp := range benchSharedTagCandidates(seedPath, seed, entries) {
			candSet[cp] = struct{}{}
		}
		for cp, a := range scores {
			if a.score >= benchCandFloor {
				candSet[cp] = struct{}{}
			}
		}
		cands := make([]string, 0, len(candSet))
		for cp := range candSet {
			cands = append(cands, cp)
		}
		sort.Strings(cands)

		dist := [4]int{}
		for _, cp := range cands {
			key := seedPath + "\x00" + cp
			lab, ok := labels[key]
			if !ok {
				g, reason := benchJudgeGrade(judgeURL, judgeModel, seedCtx, store.benchPageCtx(cp, entries[cp]))
				if g < 0 {
					t.Logf("judge failed: %s | %s", seed.Title, entries[cp].Title)
					continue
				}
				lab = benchLabel{Grade: g, Reason: reason}
				labels[key] = lab
				newLabels++
				if newLabels%5 == 0 {
					benchSaveLabels(cachePath, labels)
				}
			}
			var sc float64
			if a := scores[cp]; a != nil {
				sc = a.score
			}
			samples = append(samples, sample{score: sc, grade: lab.Grade})
			dist[lab.Grade]++
		}
		t.Logf("seed %-14s cands=%2d  grades 0/1/2/3 = %d/%d/%d/%d", q, len(cands), dist[0], dist[1], dist[2], dist[3])
	}
	benchSaveLabels(cachePath, labels)

	if len(samples) < 4 {
		t.Fatalf("only %d samples — judge unreachable?", len(samples))
	}

	xs := make([]float64, len(samples))
	ys := make([]float64, len(samples))
	for i, s := range samples {
		xs[i] = s.score
		ys[i] = float64(s.grade)
	}
	rho := spearman(xs, ys)

	var sum [4]float64
	var cnt [4]int
	for _, s := range samples {
		sum[s.grade] += s.score
		cnt[s.grade]++
	}
	var strong, weak []float64
	for _, s := range samples {
		if s.grade >= 2 {
			strong = append(strong, s.score)
		} else {
			weak = append(weak, s.score)
		}
	}
	sep := pairwiseAUC(strong, weak)

	t.Logf("──────────── GRADED BENCH (%d pairs) ────────────", len(samples))
	for g := 0; g < 4; g++ {
		mean := 0.0
		if cnt[g] > 0 {
			mean = sum[g] / float64(cnt[g])
		}
		t.Logf("grade %d (n=%2d): mean score = %.2f", g, cnt[g], mean)
	}
	t.Logf("Spearman(score, grade)      : %+.3f  (1=perfect rank agreement)", rho)
	t.Logf("strong(≥2) vs weak(≤1) AUC  : %.3f  (1=perfect separation, 0.5=random)", sep)
}

// benchPageCtx renders a page for the judge: title, tags, summary, and a body
// excerpt so it can reason over real detail (담당자/거래처/현장), not just the title.
func (s *Store) benchPageCtx(path string, e IndexEntry) string {
	body := ""
	if p, err := s.ReadPage(path); err == nil {
		body = benchClip(p.Body, 800)
	}
	return fmt.Sprintf("제목: %s\n태그: %s\n요약: %s\n본문: %s",
		e.Title, strings.Join(e.Tags, ", "), benchClip(e.Summary, 200), body)
}

func benchSharedTagCandidates(seedPath string, seed IndexEntry, entries map[string]IndexEntry) []string {
	seedTags := map[string]struct{}{}
	for _, t := range seed.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			seedTags[t] = struct{}{}
		}
	}
	var out []string
	for path, e := range entries {
		if path == seedPath {
			continue
		}
		for _, t := range e.Tags {
			if _, ok := seedTags[strings.ToLower(strings.TrimSpace(t))]; ok {
				out = append(out, path)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

var (
	benchGradeRe  = regexp.MustCompile(`GRADE\s*=\s*([0-3])`)
	benchReasonRe = regexp.MustCompile(`REASON\s*=\s*(.+)`)
)

// benchJudgeGrade rates the work-relatedness of two pages 0..3 with a one-line
// reason. Full reasoning (step3p7 is the strong model, don't hobble it) but a
// generous token budget so it always reaches the GRADE/REASON markers, plus one
// retry. Returns (grade, reason); grade -1 on failure.
func benchJudgeGrade(url, model, a, b string) (int, string) {
	prompt := "두 위키 페이지의 **업무 연관 정도**를 0~3으로 판정:\n" +
		"0 = 무관 (둘 다 태양광/케이블 산업일 뿐, 현장·거래처·담당자 모두 다름)\n" +
		"1 = 약함 (같은 거래처/담당자/지역 중 하나만 겹침, 현장·프로젝트는 다름)\n" +
		"2 = 중간 (같은 현장/사이트를 다루나 업무 항목은 다름)\n" +
		"3 = 강함 (같은 프로젝트, 또는 직접 후속·상위·의존 관계)\n" +
		"신중히 추론한 뒤, 마지막 두 줄에 정확히 이 형식으로:\nGRADE=N\nREASON=<한 문장 한국어 이유>\n\n[A]\n" + a + "\n\n[B]\n" + b

	body, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 3500, "temperature": 0,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return -1, ""
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
		if err != nil {
			continue
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		derr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if derr != nil || len(out.Choices) == 0 {
			continue
		}
		c := out.Choices[0].Message.Content
		gh := benchGradeRe.FindAllStringSubmatch(c, -1)
		if len(gh) == 0 {
			continue
		}
		reason := ""
		if rh := benchReasonRe.FindAllStringSubmatch(c, -1); len(rh) > 0 {
			reason = strings.TrimSpace(rh[len(rh)-1][1])
		}
		return int(gh[len(gh)-1][1][0] - '0'), reason
	}
	return -1, ""
}

func benchClip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func benchEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func benchLoadLabels(path string) map[string]benchLabel {
	m := map[string]benchLabel{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func benchSaveLabels(path string, m map[string]benchLabel) {
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	if raw, err := json.MarshalIndent(m, "", " "); err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}

// spearman is Pearson correlation over rank-transformed inputs (ties → average
// rank).
func spearman(xs, ys []float64) float64 { return pearson(ranksOf(xs), ranksOf(ys)) }

func ranksOf(v []float64) []float64 {
	n := len(v)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return v[idx[i]] < v[idx[j]] })
	r := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			r[idx[k]] = avg
		}
		i = j + 1
	}
	return r
}

func pearson(a, b []float64) float64 {
	n := float64(len(a))
	if n == 0 {
		return 0
	}
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= n
	mb /= n
	var cov, va, vb float64
	for i := range a {
		da, db := a[i]-ma, b[i]-mb
		cov += da * db
		va += da * da
		vb += db * db
	}
	if va == 0 || vb == 0 {
		return 0
	}
	return cov / math.Sqrt(va*vb)
}

func pairwiseAUC(strong, weak []float64) float64 {
	if len(strong) == 0 || len(weak) == 0 {
		return 0
	}
	var c float64
	for _, s := range strong {
		for _, w := range weak {
			switch {
			case s > w:
				c++
			case s == w:
				c += 0.5
			}
		}
	}
	return c / float64(len(strong)*len(weak))
}
