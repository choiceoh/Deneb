// Compaction benchmark for autoresearch optimization.
//
// Exercises the full Aurora compaction pipeline (SyncMessage, PersistLeafSummary,
// PersistCondensedSummary, Assemble) using diverse conversation scenarios and a
// deterministic mock summarizer. Produces a composite metric (0-100) that
// balances compression efficiency, information preservation, hierarchy health,
// assembly quality, and cross-scenario robustness.
//
// Overfitting prevention:
//   - 5 training + 3 holdout scenarios with holdout penalty
//   - Variance penalty across scenarios (S5: Robustness)
//   - Per-run perturbation seed for minor jitter
//   - Cross-scale validation (20-200 messages)
package aurora

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// BenchScenario is a synthetic conversation for compaction benchmarking.
type BenchScenario struct {
	Name        string
	Messages    []BenchMessage
	AnchorFacts []string // keywords that must survive compaction
	TokenBudget uint64   // budget for assembly
	IsHoldout   bool
}

// BenchMessage is a single message in a benchmark scenario.
type BenchMessage struct {
	Role       string
	Content    string
	TokenCount uint64
}

// CompactionResult holds post-compaction measurements.
type CompactionResult struct {
	TokensBefore    uint64
	TokensAfter     uint64
	LeafCount       int
	CondensedCount  int
	MaxDepth        uint32
	AssembledText   string // concatenated text from assembly
	FreshTailFound  int    // how many of the last FreshTailCount messages appear
	FreshTailTotal  int
	AssembledTokens int
}

// ScenarioScore holds per-scenario sub-scores (each 0-1).
type ScenarioScore struct {
	Name         string
	Compression  float64
	Preservation float64
	Hierarchy    float64
	Assembly     float64
}

// ── Schema (non-test copy for standalone binary) ──────────────────────────────

// benchSchemaSQL is the Aurora DDL for benchmark use (same as testSchemaSQL).
const benchSchemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sequences (
	name  TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS context_items (
	conversation_id INTEGER NOT NULL,
	ordinal         INTEGER NOT NULL,
	item_type       TEXT NOT NULL,
	message_id      INTEGER,
	summary_id      TEXT,
	created_at      INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, ordinal)
);
CREATE INDEX IF NOT EXISTS idx_ci_conv ON context_items(conversation_id);

CREATE TABLE IF NOT EXISTS messages (
	message_id      INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	seq             INTEGER NOT NULL,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	token_count     INTEGER NOT NULL,
	created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_msg_conv ON messages(conversation_id);

CREATE TABLE IF NOT EXISTS summaries (
	summary_id                TEXT PRIMARY KEY,
	conversation_id           INTEGER NOT NULL,
	kind                      TEXT NOT NULL,
	depth                     INTEGER NOT NULL DEFAULT 0,
	content                   TEXT NOT NULL,
	token_count               INTEGER NOT NULL,
	file_ids                  TEXT NOT NULL DEFAULT '[]',
	earliest_at               INTEGER,
	latest_at                 INTEGER,
	descendant_count          INTEGER NOT NULL DEFAULT 0,
	descendant_token_count    INTEGER NOT NULL DEFAULT 0,
	source_message_token_count INTEGER NOT NULL DEFAULT 0,
	created_at                INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sum_conv ON summaries(conversation_id);

CREATE TABLE IF NOT EXISTS summary_parents (
	summary_id TEXT NOT NULL,
	parent_id  TEXT NOT NULL,
	PRIMARY KEY (summary_id, parent_id)
);

CREATE TABLE IF NOT EXISTS summary_messages (
	summary_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	PRIMARY KEY (summary_id, message_id)
);

CREATE TABLE IF NOT EXISTS compaction_events (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id  INTEGER NOT NULL,
	pass             TEXT NOT NULL,
	level            TEXT NOT NULL,
	tokens_before    INTEGER NOT NULL,
	tokens_after     INTEGER NOT NULL,
	created_summary_id TEXT NOT NULL,
	created_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ce_conv ON compaction_events(conversation_id);

CREATE TABLE IF NOT EXISTS transferred_summaries (
	summary_id    TEXT PRIMARY KEY,
	transferred_at INTEGER NOT NULL
);
`

// ── Scenario generation ───────────────────────────────────────────────────────

// koreanFillers are low-information Korean sentences for padding.
var koreanFillers = []string{
	"네, 알겠습니다.",
	"그렇군요, 이해했습니다.",
	"좋은 생각이네요.",
	"계속 진행해 주세요.",
	"확인했습니다.",
	"말씀하신 부분 잘 알겠습니다.",
	"다른 질문 있으시면 말씀해 주세요.",
	"추가로 도움이 필요하시면 알려주세요.",
	"그 부분은 제가 처리하겠습니다.",
	"잠시만 기다려 주세요.",
	"검토해 보겠습니다.",
	"좋습니다, 다음 단계로 넘어가겠습니다.",
}

// englishFillers are low-information English sentences for padding.
var englishFillers = []string{
	"Sure, I understand.",
	"Let me check that for you.",
	"That makes sense.",
	"I'll look into it.",
	"Got it, moving on.",
	"Okay, let me process that.",
	"Here's what I found so far.",
	"Let me continue with the analysis.",
	"I'll handle that right away.",
	"Everything looks good so far.",
	"Working on that now.",
	"Let me verify and get back to you.",
}

// technicalFragments are code/technical snippets for padding.
var technicalFragments = []string{
	"The function processes the input buffer and returns the parsed result.",
	"We should consider using a mutex here to prevent race conditions.",
	"The database query returns paginated results with a default limit of 50.",
	"This implementation uses a binary search for O(log n) lookup time.",
	"The API endpoint accepts JSON payloads with Content-Type header.",
	"Memory allocation is handled by the pool allocator for better performance.",
	"The configuration file is loaded at startup and cached in memory.",
	"Error handling follows the standard pattern with wrapped errors.",
	"The test suite covers both happy path and edge cases.",
	"We use WAL mode for SQLite to allow concurrent reads.",
}

// koreanTechnical are Korean technical sentences.
var koreanTechnical = []string{
	"서버 재���작 없이 설정을 적용할 수 있습니다.",
	"캐시 무효화 전략을 검토해야 합니다.",
	"데이터베이스 마이그레이션은 롤백 가능하게 작성합니다.",
	"로드 밸런서 설정을 확인해 주세요.",
	"메모리 사용량이 임계값을 ��과하면 경고를 보냅니다.",
	"배포 파이프라인에 테스트 단계를 추가했습니다.",
	"API 응답 시간을 모니터링하고 있습니다.",
	"인덱스를 추가하면 쿼리 성능이 개선됩니다.",
}

// generateScenarios returns all 8 benchmark scenarios.
func generateScenarios(perturbSeed int64) []BenchScenario {
	return []BenchScenario{
		genCasualKorean20(perturbSeed),
		genTechnicalDiscussion50(perturbSeed),
		genToolHeavy100(perturbSeed),
		genMixedLanguage200(perturbSeed),
		genSparseLong100(perturbSeed),
		genCodingSession80(perturbSeed),
		genFactDense30(perturbSeed),
		genScaleTest150(perturbSeed),
	}
}

// T1: 20 messages, casual Korean, low density.
func genCasualKorean20(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x1111))
	msgs := make([]BenchMessage, 20)
	anchors := []string{"010-1234-5678", "2025년 3월 15일"}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		var content string
		switch {
		case i == 5:
			content = "제 전화번호는 010-1234-5678입니다. 연락 주세요."
		case i == 12:
			content = "다음 미팅은 2025년 3월 15일에 잡았습니다. 꼭 참석해 주세요."
		default:
			content = padContent(koreanFillers[rng.Intn(len(koreanFillers))], 80+rng.Intn(40), rng, koreanFillers)
		}
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "casualKorean20", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 500, IsHoldout: false,
	}
}

// T2: 50 messages, technical English, medium-high density.
func genTechnicalDiscussion50(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x2222))
	msgs := make([]BenchMessage, 50)
	anchors := []string{
		"func (s *Store) CompactRange(start, end uint64) error",
		"18790",
		"chose WAL mode over DELETE mode",
		"maxRetries = 5",
	}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		var content string
		switch {
		case i == 8:
			content = "The key function signature is: func (s *Store) CompactRange(start, end uint64) error — this handles range-based compaction."
		case i == 15:
			content = "The gateway runs on port 18790 in dev mode. Make sure you don't conflict with the production port."
		case i == 28:
			content = "After benchmarking, we chose WAL mode over DELETE mode because it allows concurrent reads during writes."
		case i == 40:
			content = "The retry configuration is set to maxRetries = 5 with exponential backoff starting at 1 second."
		default:
			content = padContent(technicalFragments[rng.Intn(len(technicalFragments))], 40+rng.Intn(160), rng, technicalFragments)
		}
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "technicalDiscussion50", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 2000, IsHoldout: false,
	}
}

// T3: 100 messages, tool-heavy with large results.
func genToolHeavy100(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x3333))
	msgs := make([]BenchMessage, 100)
	anchors := []string{"192.168.1.42", "a1b2c3d4", "SQLITE_BUSY", "/etc/deneb/config.yaml", "v2.4.1"}

	anchorPositions := map[int]string{
		10: "Tool result: Server IP is 192.168.1.42, responding on port 443.",
		25: "Git log shows the last commit was a1b2c3d4: fix memory leak in compaction.",
		50: "Error encountered: SQLITE_BUSY — the database is locked by another process.",
		70: "Configuration loaded from /etc/deneb/config.yaml with 12 sections parsed.",
		90: "Current deployment version is v2.4.1, deployed 3 hours ago.",
	}

	for i := range msgs {
		role := "user"
		if i%3 == 1 {
			role = "assistant"
		} else if i%3 == 2 {
			role = "assistant" // tool result
		}
		if content, ok := anchorPositions[i]; ok {
			msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
			continue
		}
		// Tool results are large, regular messages are small.
		targetLen := 30 + rng.Intn(60)
		if i%3 == 2 {
			targetLen = 200 + rng.Intn(200) // tool result
		}
		content := padContent(technicalFragments[rng.Intn(len(technicalFragments))], targetLen, rng, technicalFragments)
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "toolHeavy100", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 4000, IsHoldout: false,
	}
}

// T4: 200 messages, mixed Korean/English, high density.
func genMixedLanguage200(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x4444))
	msgs := make([]BenchMessage, 200)
	anchors := []string{
		"Project-Nebula",
		"오후 3시 30분",
		"$45,000",
		"김철수",
		"이영희",
		"https://deploy.nebula.io/v3",
		"TLS 1.3 with ECDHE-P256",
	}

	anchorPositions := map[int]string{
		12:  "프로젝트 코드명은 Project-Nebula입니다. 외부에 노출하지 마세요.",
		35:  "다음 스프린트 리뷰는 오후 3시 30분에 시작합니다.",
		60:  "이번 분기 인프라 예산은 $45,000으로 확정되었습니다.",
		88:  "김철수 님이 백엔드 리드로 합류했습니다. 온보딩 진행 중입니다.",
		120: "이영희 님이 프론트엔드 코드 리뷰를 담당합니다.",
		155: "배포 URL은 https://deploy.nebula.io/v3 입니다. 스테이징 환경도 동일합니다.",
		185: "보안 요구사항: TLS 1.3 with ECDHE-P256 필수. 하위 프로토콜 비활성화.",
	}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if content, ok := anchorPositions[i]; ok {
			msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
			continue
		}
		// Alternate Korean/English.
		targetLen := 20 + rng.Intn(280)
		fillers := koreanFillers
		if i%3 == 0 {
			fillers = englishFillers
		} else if i%3 == 1 {
			fillers = koreanTechnical
		}
		content := padContent(fillers[rng.Intn(len(fillers))], targetLen, rng, fillers)
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "mixedLanguage200", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 8000, IsHoldout: false,
	}
}

// T5: 100 messages, sparse info buried in noise.
func genSparseLong100(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x5555))
	msgs := make([]BenchMessage, 100)
	anchors := []string{"$2b$12$LJ3m4ks9xUqR", "gateway-prod-kr1.deneb.internal"}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		var content string
		switch {
		case i == 67:
			content = "참고로 비밀번호 해시는 $2b$12$LJ3m4ks9xUqR 입니다. 절대 공유하지 마세요."
		case i == 83:
			content = "프로덕션 서버 호스트명은 gateway-prod-kr1.deneb.internal 입니다."
		default:
			content = padContent(koreanFillers[rng.Intn(len(koreanFillers))], 60+rng.Intn(80), rng, koreanFillers)
		}
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "sparseLong100", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 2600, IsHoldout: false,
	}
}

// H1: 80 messages, vibe coding session.
func genCodingSession80(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x6666))
	msgs := make([]BenchMessage, 80)
	anchors := []string{"telegram-formatter", "make go-test -race", "https://staging.deneb.ai/v2"}

	anchorPositions := map[int]string{
		18: "지금 작업 중인 모듈은 telegram-formatter 입니다. 마크다운 변환 로직을 수정하고 있어요.",
		45: "테스트 명령어는 make go-test -race 입니다. 레이스 컨디션 검출을 위해 -race 플래그 필수.",
		65: "배포 확인은 https://staging.deneb.ai/v2 에서 하면 됩니다.",
	}

	codingFragments := []string{
		"파일을 수정했습니다. 빌드해 볼게요.",
		"테스트가 통과했습니다.",
		"에러가 발생했네요. 로그를 확인해 보겠습니다.",
		"리팩토링이 필요한 부분을 찾았습니다.",
		"코드 리뷰 반영 완료했습니다.",
		"새로운 기능을 추가하겠습니다.",
		"의존성 업데이트를 확인하고 있습니다.",
		"커밋 메시지를 작성하겠습니다.",
	}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if content, ok := anchorPositions[i]; ok {
			msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
			continue
		}
		content := padContent(codingFragments[rng.Intn(len(codingFragments))], 50+rng.Intn(120), rng, codingFragments)
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "codingSession80", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 2400, IsHoldout: true,
	}
}

// H2: 30 messages, fact-dense (every message has entities).
func genFactDense30(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x7777))
	msgs := make([]BenchMessage, 30)
	anchors := []string{
		"Redis 7.2",
		"PostgreSQL 16",
		"172.31.0.0/16",
		"jwt-secret-prod-2025",
		"us-west-2",
		"max_connections = 200",
	}

	// Every message contains named entities.
	factTemplates := []string{
		"Redis 7.2 클러스터는 3개 노드로 구성되어 있습니다. 마스터 1대, 리플리카 2대.",
		"PostgreSQL 16 으로 업그레이드 후 쿼리 성능이 30%% 개선되었습니다.",
		"VPC 대역은 172.31.0.0/16 입니다. 서브넷은 /24 단위로 분할.",
		"JWT 시크릿 키는 jwt-secret-prod-2025 입니다. 분기별 로테이션 예정.",
		"메인 리전은 us-west-2 입니다. DR 리전은 ap-northeast-2.",
		"커넥션 풀 설정은 max_connections = 200 입니다. 피크 시 150개 사용.",
	}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		// Embed anchors at known positions and fill rest with entity-rich content.
		if i < len(factTemplates) {
			msgs[i] = BenchMessage{Role: role, Content: factTemplates[i], TokenCount: estimateTokens(factTemplates[i])}
			continue
		}
		content := padContent(koreanTechnical[rng.Intn(len(koreanTechnical))], 60+rng.Intn(100), rng, koreanTechnical)
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "factDense30", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 800, IsHoldout: true,
	}
}

// H3: 150 messages, scaled version of T2 for cross-scale validation.
func genScaleTest150(seed int64) BenchScenario {
	rng := rand.New(rand.NewSource(seed ^ 0x8888))
	msgs := make([]BenchMessage, 150)
	// Same anchors as T2.
	anchors := []string{
		"func (s *Store) CompactRange(start, end uint64) error",
		"18790",
		"chose WAL mode over DELETE mode",
	}

	anchorPositions := map[int]string{
		20:  "The key function signature is: func (s *Store) CompactRange(start, end uint64) error — this handles range-based compaction.",
		60:  "The gateway runs on port 18790 in dev mode. Make sure you don't conflict with the production port.",
		110: "After benchmarking, we chose WAL mode over DELETE mode because it allows concurrent reads during writes.",
	}

	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if content, ok := anchorPositions[i]; ok {
			msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
			continue
		}
		content := padContent(technicalFragments[rng.Intn(len(technicalFragments))], 40+rng.Intn(160), rng, technicalFragments)
		msgs[i] = BenchMessage{Role: role, Content: content, TokenCount: estimateTokens(content)}
	}
	return BenchScenario{
		Name: "scaleTest150", Messages: msgs, AnchorFacts: anchors,
		TokenBudget: 5800, IsHoldout: true,
	}
}

// padContent repeats filler sentences until the content reaches targetRunes.
func padContent(base string, targetRunes int, rng *rand.Rand, fillers []string) string {
	if utf8.RuneCountInString(base) >= targetRunes {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	for utf8.RuneCountInString(b.String()) < targetRunes {
		b.WriteString(" ")
		b.WriteString(fillers[rng.Intn(len(fillers))])
	}
	return b.String()
}

func estimateTokens(s string) uint64 {
	n := utf8.RuneCountInString(s) / 2
	if n < 1 {
		return 1
	}
	return uint64(n)
}

// ── Mock Summarizer ───────────────────────────────────���───────────────────────

// Regex patterns for detecting high-information content.
var (
	reNumbers   = regexp.MustCompile(`\d{2,}`)
	rePaths     = regexp.MustCompile(`[/\\]\w+[/\\]\w+`)
	reURLs      = regexp.MustCompile(`https?://\S+`)
	reCode      = regexp.MustCompile(`func\s+\w+|var\s+\w+|\w+\.\w+\(`)
	reIPAddr    = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	reHashLike  = regexp.MustCompile(`[a-f0-9]{6,}`)
	reAssign    = regexp.MustCompile(`\w+\s*=\s*\S+`)
	reKorFiller = regexp.MustCompile(`^(네|알겠습니다|좋습니다|확인했습니다|그렇군요)\b`)
	reEngFiller = regexp.MustCompile(`^(Sure|Got it|Okay|Let me)\b`)
)

// newMockSummarizer returns a deterministic summarizer that preserves
// high-information sentences and discards filler. It does NOT have
// access to anchor fact lists (no cheating).
func newMockSummarizer() Summarizer {
	return func(text string, aggressive bool, opts *SummarizeOptions) (string, error) {
		targetTokens := uint32(600)
		if opts != nil && opts.TargetTokens != nil {
			targetTokens = *opts.TargetTokens
		}
		if aggressive {
			targetTokens = targetTokens * 60 / 100
		}

		// Split into sentences.
		sentences := splitSentences(text)
		if len(sentences) == 0 {
			return "<summary>" + text + "</summary>", nil
		}

		// Score each sentence by information density.
		type scored struct {
			idx   int
			text  string
			score int
		}
		scoredSentences := make([]scored, len(sentences))
		for i, s := range sentences {
			score := 0
			if reNumbers.MatchString(s) {
				score += 3
			}
			if rePaths.MatchString(s) {
				score += 3
			}
			if reURLs.MatchString(s) {
				score += 3
			}
			if reCode.MatchString(s) {
				score += 2
			}
			if reIPAddr.MatchString(s) {
				score += 3
			}
			if reHashLike.MatchString(s) {
				score += 2
			}
			if reAssign.MatchString(s) {
				score += 2
			}
			if reKorFiller.MatchString(s) {
				score -= 2
			}
			if reEngFiller.MatchString(s) {
				score -= 2
			}
			// Base score from length (longer = more likely informative).
			score += utf8.RuneCountInString(s) / 40
			scoredSentences[i] = scored{idx: i, text: s, score: score}
		}

		// Sort by score descending.
		sort.Slice(scoredSentences, func(i, j int) bool {
			return scoredSentences[i].score > scoredSentences[j].score
		})

		// Greedily select until target tokens reached.
		var selected []scored
		currentTokens := uint32(0)
		for _, s := range scoredSentences {
			st := uint32(estimateTokens(s.text))
			if currentTokens+st > targetTokens && len(selected) > 0 {
				break
			}
			selected = append(selected, s)
			currentTokens += st
		}

		// Restore original order.
		sort.Slice(selected, func(i, j int) bool {
			return selected[i].idx < selected[j].idx
		})

		var b strings.Builder
		b.WriteString("<summary>")
		for i, s := range selected {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(s.text)
		}
		b.WriteString("</summary>")
		return b.String(), nil
	}
}

// splitSentences splits text on periods, newlines, and Korean period.
func splitSentences(text string) []string {
	// Split on sentence boundaries.
	raw := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n'
	})
	var sentences []string
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Further split on ". " and "。" but keep the content.
		parts := strings.Split(line, ". ")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				sentences = append(sentences, p)
			}
		}
	}
	return sentences
}

// ── Compaction Driver (pure Go, no FFI) ───────────────────────────────────────

// driveCompaction runs compaction using Aurora store APIs and the given config.
// This replicates the Rust sweep logic in pure Go for benchmarking.
func driveCompaction(
	store *Store,
	convID uint64,
	budget uint64,
	cfg SweepConfig,
	summarizer Summarizer,
) (*CompactionResult, error) {
	tokensBefore, err := store.FetchTokenCount(convID)
	if err != nil {
		return nil, err
	}

	result := &CompactionResult{TokensBefore: tokensBefore}
	leafCounter := 0
	condensedCounter := 0

	for round := uint32(0); round < cfg.MaxRounds; round++ {
		currentTokens, err := store.FetchTokenCount(convID)
		if err != nil {
			return nil, err
		}

		// Check if compaction is needed: tokens > threshold * budget.
		threshold := uint64(float64(cfg.ContextThreshold) * float64(budget))
		if currentTokens <= threshold {
			break // under threshold, no compaction needed
		}

		items, err := store.FetchContextItems(convID)
		if err != nil {
			return nil, err
		}

		// Collect only message items (not summaries) for leaf compaction.
		var messageItems []ContextItem
		var summaryItems []ContextItem
		for _, ci := range items {
			if ci.ItemType == "message" {
				messageItems = append(messageItems, ci)
			} else if ci.ItemType == "summary" {
				summaryItems = append(summaryItems, ci)
			}
		}

		// ── Phase 1: Leaf compaction ──
		// Skip the last FreshTailCount messages.
		compactableMessages := messageItems
		if len(compactableMessages) > int(cfg.FreshTailCount) {
			compactableMessages = compactableMessages[:len(compactableMessages)-int(cfg.FreshTailCount)]
		} else {
			compactableMessages = nil
		}

		if len(compactableMessages) >= int(cfg.LeafMinFanout) {
			// Chunk into groups of LeafMinFanout.
			for start := 0; start+int(cfg.LeafMinFanout) <= len(compactableMessages); start += int(cfg.LeafMinFanout) {
				end := start + int(cfg.LeafMinFanout)
				chunk := compactableMessages[start:end]

				// Fetch message contents.
				var msgIDs []uint64
				for _, ci := range chunk {
					if ci.MessageID != nil {
						msgIDs = append(msgIDs, *ci.MessageID)
					}
				}
				msgRecords, err := store.FetchMessages(msgIDs)
				if err != nil {
					return nil, err
				}

				// Build text for summarization.
				var textBuf strings.Builder
				var srcTokens uint64
				for _, ci := range chunk {
					if ci.MessageID == nil {
						continue
					}
					if m, ok := msgRecords[*ci.MessageID]; ok {
						fmt.Fprintf(&textBuf, "[%s]: %s\n", m.Role, m.Content)
						srcTokens += m.TokenCount
					}
				}

				targetTokens := cfg.LeafTargetTokens
				summary, err := summarizer(textBuf.String(), false, &SummarizeOptions{
					TargetTokens: &targetTokens,
				})
				if err != nil {
					return nil, fmt.Errorf("leaf summarize: %w", err)
				}

				leafCounter++
				sumID := fmt.Sprintf("bench_leaf_%d", leafCounter)
				sumTokens := estimateTokens(summary)

				err = store.PersistLeafSummary(PersistLeafInput{
					SummaryID:               sumID,
					ConversationID:          convID,
					Content:                 summary,
					TokenCount:              sumTokens,
					FileIDs:                 []string{},
					SourceMessageTokenCount: srcTokens,
					MessageIDs:              msgIDs,
					StartOrdinal:            chunk[0].Ordinal,
					EndOrdinal:              chunk[len(chunk)-1].Ordinal,
				})
				if err != nil {
					return nil, fmt.Errorf("persist leaf: %w", err)
				}
			}
		}

		// ── Phase 2: Condensed compaction ──
		// Re-fetch items after leaf phase.
		items, err = store.FetchContextItems(convID)
		if err != nil {
			return nil, err
		}
		summaryItems = nil
		for _, ci := range items {
			if ci.ItemType == "summary" {
				summaryItems = append(summaryItems, ci)
			}
		}

		if len(summaryItems) >= int(cfg.CondensedMinFanout) {
			// Group summaries for condensation.
			for start := 0; start+int(cfg.CondensedMinFanout) <= len(summaryItems); start += int(cfg.CondensedMinFanout) {
				end := start + int(cfg.CondensedMinFanout)
				chunk := summaryItems[start:end]

				var sumIDs []string
				for _, ci := range chunk {
					if ci.SummaryID != nil {
						sumIDs = append(sumIDs, *ci.SummaryID)
					}
				}
				sumRecords, err := store.FetchSummaries(sumIDs)
				if err != nil {
					return nil, err
				}

				var textBuf strings.Builder
				var descTokens uint64
				var descCount uint64
				var srcMsgTokens uint64
				maxDepth := uint32(0)
				for _, sid := range sumIDs {
					if s, ok := sumRecords[sid]; ok {
						textBuf.WriteString(s.Content)
						textBuf.WriteString("\n\n")
						descTokens += s.TokenCount
						descCount += s.DescendantCount + 1
						srcMsgTokens += s.SourceMessageTokenCount
						if s.Depth >= maxDepth {
							maxDepth = s.Depth
						}
					}
				}

				targetTokens := cfg.CondensedTargetTokens
				summary, err := summarizer(textBuf.String(), true, &SummarizeOptions{
					IsCondensed:  ptrBool(true),
					Depth:        ptrUint32(maxDepth + 1),
					TargetTokens: &targetTokens,
				})
				if err != nil {
					return nil, fmt.Errorf("condensed summarize: %w", err)
				}

				condensedCounter++
				cID := fmt.Sprintf("bench_condensed_%d", condensedCounter)
				cTokens := estimateTokens(summary)

				err = store.PersistCondensedSummary(PersistCondensedInput{
					SummaryID:               cID,
					ConversationID:          convID,
					Depth:                   maxDepth + 1,
					Content:                 summary,
					TokenCount:              cTokens,
					FileIDs:                 []string{},
					DescendantCount:         descCount,
					DescendantTokenCount:    descTokens,
					SourceMessageTokenCount: srcMsgTokens,
					ParentSummaryIDs:        sumIDs,
					StartOrdinal:            chunk[0].Ordinal,
					EndOrdinal:              chunk[len(chunk)-1].Ordinal,
				})
				if err != nil {
					return nil, fmt.Errorf("persist condensed: %w", err)
				}
			}
		}

		// Check if we made progress this round.
		newTokens, _ := store.FetchTokenCount(convID)
		if newTokens >= currentTokens {
			break // no progress, stop
		}
	}

	// ── Post-compaction measurement ──
	result.TokensAfter, _ = store.FetchTokenCount(convID)
	stats, _ := store.FetchSummaryStats(convID)
	result.LeafCount = stats.LeafCount
	result.CondensedCount = stats.CondensedCount
	result.MaxDepth = stats.MaxDepth

	return result, nil
}

func ptrBool(v bool) *bool       { return &v }
func ptrUint32(v uint32) *uint32 { return &v }

// ── Assembly and scoring ──────────────────────────────────────────────────────

// runScenario loads a scenario into a fresh store, runs compaction, assembles,
// and returns the result.
func runScenario(scenario BenchScenario, cfg SweepConfig) (*CompactionResult, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(benchSchemaSQL); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	store, err := NewStoreFromDB(db, slog.Default())
	if err != nil {
		return nil, err
	}

	convID := uint64(1)

	// Sync all messages.
	for _, msg := range scenario.Messages {
		if _, err := store.SyncMessage(convID, msg.Role, msg.Content, msg.TokenCount); err != nil {
			return nil, fmt.Errorf("sync message: %w", err)
		}
	}

	// Run compaction.
	summarizer := newMockSummarizer()
	result, err := driveCompaction(store, convID, scenario.TokenBudget, cfg, summarizer)
	if err != nil {
		return nil, fmt.Errorf("compaction: %w", err)
	}

	// Assemble context (fallback mode, no FFI).
	asmCfg := AssemblyConfig{
		TokenBudget:    scenario.TokenBudget,
		FreshTailCount: cfg.FreshTailCount,
		MaxMessages:    1000,
	}
	asmResult, err := assembleFallback(store, convID, asmCfg, slog.Default())
	if err != nil {
		return nil, fmt.Errorf("assembly: %w", err)
	}

	// Build assembled text for anchor checking.
	// llm.Message.Content is json.RawMessage (JSON string or []ContentBlock).
	var assembled strings.Builder
	for _, msg := range asmResult.Messages {
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			assembled.WriteString(text)
		} else {
			// Fallback: raw content.
			assembled.Write(msg.Content)
		}
		assembled.WriteString("\n")
	}
	result.AssembledText = assembled.String()
	result.AssembledTokens = asmResult.EstimatedTokens
	if result.AssembledTokens == 0 {
		// Fallback doesn't set EstimatedTokens, estimate from text.
		result.AssembledTokens = int(estimateTokens(result.AssembledText))
	}

	// Check fresh tail presence.
	tailCount := int(cfg.FreshTailCount)
	if tailCount > len(scenario.Messages) {
		tailCount = len(scenario.Messages)
	}
	result.FreshTailTotal = tailCount
	for i := len(scenario.Messages) - tailCount; i < len(scenario.Messages); i++ {
		if strings.Contains(result.AssembledText, scenario.Messages[i].Content) {
			result.FreshTailFound++
		}
	}

	return result, nil
}

// scoreScenario computes per-scenario sub-scores (each 0-1).
func scoreScenario(result *CompactionResult, scenario BenchScenario) ScenarioScore {
	ss := ScenarioScore{Name: scenario.Name}

	// S1: Compression efficiency.
	if result.TokensBefore > 0 {
		ratio := 1.0 - float64(result.TokensAfter)/float64(result.TokensBefore)
		ss.Compression = clamp(ratio/0.8, 0, 1)
	}

	// S2: Information preservation (anchor survival).
	if len(scenario.AnchorFacts) > 0 {
		survived := 0
		for _, anchor := range scenario.AnchorFacts {
			if strings.Contains(result.AssembledText, anchor) {
				survived++
			}
		}
		ss.Preservation = float64(survived) / float64(len(scenario.AnchorFacts))
	}

	// S3: Hierarchy health.
	if result.LeafCount > 0 || result.CondensedCount > 0 {
		// Depth penalty: depths > 3 start to lose points.
		depthScore := 1.0
		if result.MaxDepth > 3 {
			depthScore = clamp(1.0-float64(result.MaxDepth-3)/5.0, 0, 1)
		}
		// Condensed ratio: having some condensation is good.
		condensedScore := 0.5 // default: no condensation = 0.5
		if result.CondensedCount > 0 && result.LeafCount > 0 {
			condensedScore = 1.0
		}
		ss.Hierarchy = depthScore*0.5 + condensedScore*0.5
	} else {
		// No compaction happened at all.
		ss.Hierarchy = 0.2
	}

	// S4: Assembly quality.
	freshTailScore := 0.0
	if result.FreshTailTotal > 0 {
		freshTailScore = float64(result.FreshTailFound) / float64(result.FreshTailTotal)
	}
	budgetUsage := 0.0
	if scenario.TokenBudget > 0 {
		budgetUsage = clamp(float64(result.AssembledTokens)/float64(scenario.TokenBudget), 0, 1)
	}
	ss.Assembly = freshTailScore*0.6 + budgetUsage*0.4

	return ss
}

// scenarioTotal returns the weighted sum of a single scenario's sub-scores
// (without robustness, which is cross-scenario).
func scenarioTotal(ss ScenarioScore) float64 {
	return 0.25*ss.Compression + 0.375*ss.Preservation + 0.1875*ss.Hierarchy + 0.1875*ss.Assembly
}

// compositeMetric computes the final 0-100 metric from all scenario scores.
func compositeMetric(trainScores, holdoutScores []ScenarioScore) float64 {
	if len(trainScores) == 0 {
		return 0
	}

	// Per-scenario totals for train and holdout.
	trainTotals := make([]float64, len(trainScores))
	for i, ss := range trainScores {
		trainTotals[i] = scenarioTotal(ss)
	}
	holdoutTotals := make([]float64, len(holdoutScores))
	for i, ss := range holdoutScores {
		holdoutTotals[i] = scenarioTotal(ss)
	}

	trainMean := mean(trainTotals)
	trainStd := stddev(trainTotals)

	// S5: Robustness (variance penalty across train scenarios).
	robustness := 1.0
	if trainMean > 0 {
		cv := trainStd / trainMean
		robustness = clamp(1.0-cv*3, 0, 1)
	}

	// Raw score: 80% per-scenario quality + 20% robustness.
	rawScore := trainMean*0.8 + robustness*0.2

	// Holdout penalty.
	if len(holdoutTotals) > 0 {
		holdoutMean := mean(holdoutTotals)
		gap := trainMean - holdoutMean
		if gap > 0 {
			rawScore -= gap * 2
		}
	}

	return clamp(rawScore*100, 0, 100)
}

// ── Public entry point ────────────────────────────────────────────────────────

// RunCompactionBenchmark runs all scenarios with the given SweepConfig and
// returns a composite metric (0-100). Higher is better.
// Averages over multiple perturbation seeds for robust evaluation.
func RunCompactionBenchmark(cfg SweepConfig) float64 {
	baseSeed := time.Now().Unix() / 10
	return RunCompactionBenchmarkSeeded(cfg, baseSeed)
}

// RunCompactionBenchmarkSeeded runs the benchmark with explicit seed control.
// Averages over 3 seeds for robustness against perturbation noise.
func RunCompactionBenchmarkSeeded(cfg SweepConfig, baseSeed int64) float64 {
	const seedCount = 3
	total := 0.0
	for i := int64(0); i < seedCount; i++ {
		seed := baseSeed + i*7919 // prime offset for diversity
		total += runBenchOnce(cfg, seed)
	}
	return total / seedCount
}

func runBenchOnce(cfg SweepConfig, perturbSeed int64) float64 {
	scenarios := generateScenarios(perturbSeed)

	var trainScores, holdoutScores []ScenarioScore
	for _, s := range scenarios {
		result, err := runScenario(s, cfg)
		if err != nil {
			score := ScenarioScore{Name: s.Name}
			if s.IsHoldout {
				holdoutScores = append(holdoutScores, score)
			} else {
				trainScores = append(trainScores, score)
			}
			continue
		}
		score := scoreScenario(result, s)
		if s.IsHoldout {
			holdoutScores = append(holdoutScores, score)
		} else {
			trainScores = append(trainScores, score)
		}
	}

	return compositeMetric(trainScores, holdoutScores)
}

// ── Math helpers ──────────────────────────────────────────────────────────────

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	sumSq := 0.0
	for _, x := range xs {
		d := x - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(xs)))
}
