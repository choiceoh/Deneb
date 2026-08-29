package server

// Mail arrival context bench — does the analysis prompt actually receive the
// material that was wired for it?
//
// The arrival pipeline assembles memory itself rather than going through the
// recall preflight, so none of the recall harnesses cover it. Its inputs are
// wired one function at a time (sender identity, then topic recall in #4939,
// then project state for approvals in #4940) and each was justified by
// mechanism, never by a number: nothing reported how often a wired input
// actually fires on real mail.
//
// This measures exactly that and nothing more. It is a FIRE-RATE bench, not a
// quality bench: an input that never resolves cannot help the analysis, and an
// input that resolves on 90% of mail is worth its prompt budget. Answer quality
// needs a judge and a gold set that do not exist for this lane yet.
//
// Run manually against the operator's real archive (never in CI):
//
//	DENEB_MAIL_BENCH_STORE=~/.deneb/mailstore/messages \
//	DENEB_WIKI_DIR=~/.deneb/bench/wiki-probe-copy \
//	DENEB_EMBEDDING_URL=http://127.0.0.1:8002 \
//	  go test ./internal/runtime/server/ -run TestMailArrivalContextBench -v

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type benchMail struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Text    string `json:"text"`
}

func (m benchMail) body() string {
	if strings.TrimSpace(m.Body) != "" {
		return m.Body
	}
	return m.Text
}

// loadBenchMails reads the newest archived messages, newest file first.
func loadBenchMails(t *testing.T, dir string, limit int) []benchMail {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("mail store unreadable: %v", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	var out []benchMail
	for _, name := range files {
		if len(out) >= limit {
			break
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() && len(out) < limit {
			var m benchMail
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if strings.TrimSpace(m.Subject) == "" {
				continue
			}
			out = append(out, m)
		}
		f.Close()
	}
	return out
}

func TestMailArrivalContextBench(t *testing.T) {
	storeDir := strings.TrimSpace(os.Getenv("DENEB_MAIL_BENCH_STORE"))
	wikiDir := strings.TrimSpace(os.Getenv("DENEB_WIKI_DIR"))
	if storeDir == "" || wikiDir == "" {
		t.Skip("set DENEB_MAIL_BENCH_STORE and DENEB_WIKI_DIR (use a COPY of the wiki)")
	}
	limit := 200
	if raw := strings.TrimSpace(os.Getenv("DENEB_MAIL_BENCH_LIMIT")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	mails := loadBenchMails(t, storeDir, limit)
	if len(mails) == 0 {
		t.Skip("no archived mail to measure")
	}
	store, err := wiki.NewStore(wikiDir, os.Getenv("DENEB_DIARY_DIR"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	srv := &Server{MemorySubsystem: &MemorySubsystem{wikiStore: store}}

	var senderHit, topicHit, projectHit, either int
	topicChars := 0
	for _, m := range mails {
		sender := strings.TrimSpace(srv.wikiSenderFacts(context.Background(), m.From))
		topic := strings.TrimSpace(srv.wikiTopicFacts(context.Background(), m.Subject, m.body()))
		project := ""
		if ref, ok := resolveApprovalProject(store, m.Subject, m.body()); ok {
			project = approvalProjectStateContext(store, ref)
		}
		if sender != "" {
			senderHit++
		}
		if topic != "" {
			topicHit++
			topicChars += len(topic)
		}
		if project != "" {
			projectHit++
		}
		if sender != "" || topic != "" {
			either++
		}
	}
	n := float64(len(mails))
	pct := func(v int) float64 { return 100 * float64(v) / n }
	avgTopic := 0
	if topicHit > 0 {
		avgTopic = topicChars / topicHit
	}
	t.Logf("MAIL_CONTEXT n=%d sender=%.1f%% topic=%.1f%% project-state=%.1f%% any=%.1f%% topic-chars-avg=%d",
		len(mails), pct(senderHit), pct(topicHit), pct(projectHit), pct(either), avgTopic)

	// The bench exists to catch an input that never fires. A wired input at 0%
	// is dead wiring, which is the failure this lane had no way to see.
	if topicHit == 0 {
		t.Error("topic recall never fired on real mail — wired but dead")
	}
	if senderHit == 0 {
		t.Error("sender identity never resolved on real mail — wired but dead")
	}
}

// --- quality: does topic recall reach the RIGHT page? ---

type mailGoldCase struct {
	ID        string   `json:"id"`
	Subject   string   `json:"subject"`
	From      string   `json:"from"`
	Body      string   `json:"body"`
	GoldPaths []string `json:"gold_paths"`
	Note      string   `json:"note"`
}

// TestMailArrivalContextQuality scores topic recall against hand-matched gold.
//
// The fire-rate bench above answers "does the wiring run" and got 100% for
// topic recall, which is exactly the shape that means nothing on its own: a
// retriever that always returns something scores 100% whether or not it
// returned the right thing. This is the other half — each mail was read and
// matched to its project folder by hand, and the noise class (newsletters,
// vendor billing, internal test mail) carries an EMPTY gold list because the
// correct behavior there is to recall no project at all.
//
//	DENEB_MAIL_GOLD=~/.deneb/bench/mail-context-gold.jsonl \
//	DENEB_WIKI_DIR=~/.deneb/bench/wiki-probe-copy \
//	DENEB_EMBEDDING_URL=http://127.0.0.1:8002 \
//	  go test ./internal/runtime/server/ -run TestMailArrivalContextQuality -v
func TestMailArrivalContextQuality(t *testing.T) {
	goldPath := strings.TrimSpace(os.Getenv("DENEB_MAIL_GOLD"))
	wikiDir := strings.TrimSpace(os.Getenv("DENEB_WIKI_DIR"))
	if goldPath == "" || wikiDir == "" {
		t.Skip("set DENEB_MAIL_GOLD and DENEB_WIKI_DIR (use a COPY of the wiki)")
	}
	f, err := os.Open(goldPath)
	if err != nil {
		t.Skipf("gold unreadable: %v", err)
	}
	defer f.Close()
	var cases []mailGoldCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c mailGoldCase
		if json.Unmarshal([]byte(line), &c) == nil {
			cases = append(cases, c)
		}
	}
	if len(cases) == 0 {
		t.Skip("gold set empty")
	}
	store, err := wiki.NewStore(wikiDir, os.Getenv("DENEB_DIARY_DIR"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	srv := &Server{MemorySubsystem: &MemorySubsystem{wikiStore: store}}

	var posTotal, posHit, negTotal, negQuiet int
	for _, c := range cases {
		facts := srv.wikiTopicFacts(context.Background(), c.Subject, c.Body)
		if len(c.GoldPaths) > 0 {
			posTotal++
			hit := false
			for _, g := range c.GoldPaths {
				if g != "" && strings.Contains(facts, g) {
					hit = true
					break
				}
			}
			if hit {
				posHit++
			} else {
				t.Logf("  MISS %-28s want %v", c.ID, c.GoldPaths)
			}
			continue
		}
		// Noise: recalling a project page for a newsletter is the failure.
		negTotal++
		if !strings.Contains(facts, "프로젝트/") {
			negQuiet++
		} else {
			t.Logf("  NOISE %-27s pulled a project page", c.ID)
		}
	}
	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return 100 * float64(a) / float64(b)
	}
	t.Logf("MAIL_QUALITY topic-correct=%.1f%% (%d/%d)  noise-quiet=%.1f%% (%d/%d)",
		pct(posHit, posTotal), posHit, posTotal, pct(negQuiet, negTotal), negQuiet, negTotal)
}
