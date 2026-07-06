// plaud_recordings.go — autonomous meeting-recording analysis (Plaud MCP).
//
// The mail pipeline gives every inbound mail a full analysis pass; recordings
// deserved the same but only had a pull path (chat tools, on request) plus
// whatever Plaud's AutoFlow mails happened to deliver through the fragile
// mail edge. This service closes that gap from the recorder side: it polls
// the Plaud MCP tools the gateway already registers (mcp_external_tools.go),
// detects new recordings, pulls the speaker-attributed transcript, runs a
// meeting-shaped synthesis (analysis role — the user reads this), and lands
// the result everywhere the flywheel expects:
//
//   - a 회의록/ wiki page (per linked project, else the category bucket),
//   - one dated status bullet on each linked project 대표페이지,
//   - a work-feed card (SourceMeetingReport, feed-only doctrine).
//
// Ordering with AutoFlow mails: both paths may cover the same meeting. The
// mail path stays untouched (it is the fallback when MCP auth lapses); this
// service is the deep, meeting-shaped one. Dedup of the mail-side card is a
// possible follow-up, deliberately not attempted here.
//
// First run seeds a baseline: every recording that exists before the service
// first ticks is marked seen WITHOUT analysis — months of backlog (and
// Plaud's onboarding samples) must not burn analysis tokens on deploy day.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	plaudPollInterval = 15 * time.Minute
	plaudStateFile    = "plaud-recordings-state.json"
	// Tool names as registered by mcp_external_tools.go (<server>_<tool>).
	plaudListTool       = "plaud_list_files"
	plaudTranscriptTool = "plaud_get_transcript"
	plaudToolTimeout    = 3 * time.Minute
	// plaudListWindowDays bounds each list call; generous so a multi-day
	// gateway outage never loses recordings, tiny next to the full library.
	plaudListWindowDays = 7
	// plaudMaxPerTick bounds one tick's LLM work; the unprocessed rest stays
	// unseen and comes around next tick.
	plaudMaxPerTick = 3
	// plaudMinDuration skips accidental taps; short voice memos still pass.
	plaudMinDuration = 2 * time.Minute
	// plaudMinTranscriptRunes: below this the ASR heard nothing worth a
	// synthesis call (noise, silence) — mark seen, skip.
	plaudMinTranscriptRunes = 200
	// Transcripts under the direct limit go to the synthesis model whole;
	// longer ones are map-reduced (chunk gists via the local stage-1 model,
	// then one synthesis over the gists) so a 2-hour workshop cannot blow the
	// analysis model's context or cost.
	plaudDirectRunes    = 28000
	plaudChunkRunes     = 12000
	plaudChunkMaxTokens = 500
	// plaudSynthesisTokens covers the report plus the 표기 교정 appendix; the
	// first-tick incident showed 1600 is tight even for the report alone.
	plaudSynthesisTokens   = 2800
	plaudMaxCandidates     = 40 // project candidates offered to the model
	plaudStateRetention    = 180 * 24 * time.Hour
	plaudAuthNotifyEvery   = 24 * time.Hour
	plaudRelatedProjectCap = 3
)

// plaudRecordingsDisableEnv kills the service without a deploy (incident lever).
const plaudRecordingsDisableEnv = "DENEB_PLAUD_RECORDINGS_DISABLE"

// plaudFile is one recording row from plaud_list_files.
type plaudFile struct {
	ID       string
	Name     string
	StartAt  time.Time
	Duration time.Duration
}

type plaudRecordingsState struct {
	Version int `json:"version"`
	// Baselined marks the one-time seed pass (see package comment).
	Baselined bool `json:"baselined"`
	// Seen maps recording ID → processed-at unix millis.
	Seen map[string]int64 `json:"seen"`
	// LastAuthNotify throttles the "token expired" operator card.
	LastAuthNotify int64 `json:"lastAuthNotify,omitempty"`
}

// plaudRecordingsService polls Plaud via the chat ToolRegistry and lands
// meeting analyses in the wiki + work feed. Mirrors meetingHarvestService's
// lifecycle (nil-safe start, safego goroutine, ShutdownCtx-bound).
type plaudRecordingsService struct {
	execTool func(ctx context.Context, name string, args json.RawMessage) (string, error)
	// synthesize runs the analysis-role model (user-facing report; cloud OK).
	synthesize func(ctx context.Context, system, user string, maxTokens int) (string, error)
	// gist runs the local stage-1 model for map-reduce chunk summaries.
	gist       func(ctx context.Context, system, user string, maxTokens int) (string, error)
	candidates func() []mailanalysis.ProjectCandidate
	// topic returns the 업무 topic-knowledge block (company, org, key people)
	// injected into the synthesis prompt — the subtractive slice of the main
	// agent context that measurably improves term/name correction. Empty when
	// topics are unconfigured; nil skips injection entirely.
	topic     func() string
	writePage func(relPath string, page *wiki.Page) error
	// appendStatus prepends a dated bullet on a linked project rep page
	// (wiki.Store.AppendProjectStatusLine; idempotent by ref).
	appendStatus func(projectPath, line, ref string, now time.Time) error
	// deliver posts the work-feed card (feed-only: reports are not questions,
	// so no transcript mirror — the wiki page is the durable context).
	deliver func(text string) (bool, error)

	logger     *slog.Logger
	statePath  string // "" → in-memory only (tests)
	displayLoc *time.Location
	now        func() time.Time

	mu    sync.Mutex
	state plaudRecordingsState
}

func newPlaudRecordingsService(
	execTool func(ctx context.Context, name string, args json.RawMessage) (string, error),
	synthesize func(ctx context.Context, system, user string, maxTokens int) (string, error),
	gist func(ctx context.Context, system, user string, maxTokens int) (string, error),
	candidates func() []mailanalysis.ProjectCandidate,
	topic func() string,
	writePage func(relPath string, page *wiki.Page) error,
	appendStatus func(projectPath, line, ref string, now time.Time) error,
	deliver func(text string) (bool, error),
	statePath string,
	logger *slog.Logger,
) *plaudRecordingsService {
	if execTool == nil || synthesize == nil || writePage == nil || deliver == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.FixedZone("KST", kstFallbackOffset)
	}
	s := &plaudRecordingsService{
		execTool:     execTool,
		synthesize:   synthesize,
		gist:         gist,
		candidates:   candidates,
		topic:        topic,
		writePage:    writePage,
		appendStatus: appendStatus,
		deliver:      deliver,
		logger:       logger,
		statePath:    statePath,
		displayLoc:   loc,
		now:          time.Now,
		state:        plaudRecordingsState{Version: 1, Seen: map[string]int64{}},
	}
	s.loadState()
	return s
}

func (s *plaudRecordingsService) start(ctx context.Context) {
	if s == nil {
		return
	}
	safego.GoWithSlog(s.logger, "plaud-recordings", func() {
		ticker := time.NewTicker(plaudPollInterval)
		defer ticker.Stop()
		// No immediate first pass: MCP tool discovery is async post-boot
		// (mcp_external_tools.go), so the first tick would race an empty
		// registry on every deploy and log a spurious skip.
		for {
			select {
			case <-ctx.Done():
				s.logger.Debug("plaud recordings service stopped")
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	})
}

func (s *plaudRecordingsService) tick(ctx context.Context) {
	now := s.now()
	listCtx, cancel := context.WithTimeout(ctx, plaudToolTimeout)
	args, _ := json.Marshal(map[string]any{
		"date_from": now.UTC().AddDate(0, 0, -plaudListWindowDays).Format("2006-01-02"),
		"date_to":   now.UTC().Format("2006-01-02"),
	})
	out, err := s.execTool(listCtx, plaudListTool, args)
	cancel()
	if err != nil {
		s.handleToolError(err)
		return
	}
	files, err := parsePlaudList(out)
	if err != nil {
		s.logger.Warn("plaud recordings: list parse failed", "error", err)
		return
	}
	if !s.baselined() {
		s.seedBaseline(files, now)
		return
	}

	fresh := s.selectNew(files)
	for _, f := range fresh {
		if ctx.Err() != nil {
			return
		}
		if err := s.processRecording(ctx, f, now); err != nil {
			// Leave unseen: transient failures (LLM/tool timeouts) retry next
			// tick. Persistent poison is bounded by plaudMaxPerTick.
			s.logger.Error("plaud recordings: analysis failed (will retry)",
				"id", f.ID, "name", f.Name, "error", err)
			continue
		}
		s.markSeen(f.ID, s.now())
	}
	s.pruneState(now)
}

// selectNew returns unseen, meeting-sized recordings, oldest first, capped.
func (s *plaudRecordingsService) selectNew(files []plaudFile) []plaudFile {
	var out []plaudFile
	for _, f := range files {
		if f.ID == "" || s.seen(f.ID) {
			continue
		}
		if f.Duration < plaudMinDuration {
			// Too short to analyze — remember it so it never re-surfaces.
			s.markSeen(f.ID, s.now())
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartAt.Before(out[j].StartAt) })
	if len(out) > plaudMaxPerTick {
		out = out[:plaudMaxPerTick]
	}
	return out
}

func (s *plaudRecordingsService) processRecording(ctx context.Context, f plaudFile, now time.Time) error {
	trCtx, cancel := context.WithTimeout(ctx, plaudToolTimeout)
	args, _ := json.Marshal(map[string]string{"file_id": f.ID})
	raw, err := s.execTool(trCtx, plaudTranscriptTool, args)
	cancel()
	if err != nil {
		return fmt.Errorf("get_transcript: %w", err)
	}
	transcript := parsePlaudTranscript(raw)
	if utf8.RuneCountInString(transcript) < plaudMinTranscriptRunes {
		// Nothing worth a synthesis call — a silent or noise-only recording.
		s.logger.Info("plaud recordings: transcript too short, skipping analysis",
			"id", f.ID, "name", f.Name, "runes", utf8.RuneCountInString(transcript))
		s.markSeen(f.ID, now)
		return nil
	}

	cands := s.projectCandidates()
	report, err := s.analyzeMeeting(ctx, f, transcript, cands)
	if err != nil {
		return fmt.Errorf("synthesis: %w", err)
	}
	report, related := splitRelatedProjects(report, cands)

	// Wiki page — one per recording, project slot when linked.
	project := ""
	if len(related) > 0 {
		if name, ok := wiki.ProjectNameOf(related[0]); ok {
			project = name
		}
	}
	pagePath := wiki.MeetingPagePath(project, meetingFilename(f))
	page := buildMeetingPage(f, report, related, transcript, now, s.displayLoc)
	if err := s.writePage(pagePath, page); err != nil {
		return fmt.Errorf("wiki write: %w", err)
	}

	// Dated bullet on each linked project (idempotent by recording id).
	if s.appendStatus != nil {
		line := meetingStatusLine(f, s.displayLoc)
		for _, p := range related {
			if err := s.appendStatus(p, line, "plaud:"+f.ID, now); err != nil {
				s.logger.Warn("plaud recordings: project status append failed",
					"project", p, "error", err)
			}
		}
	}

	// Work-feed card. Delivery failure is not an analysis failure: the wiki
	// page exists — log loudly (logging.md: 무응답 실패는 Error) but don't retry
	// the whole recording just to re-post a card.
	body := meetingCardBody(f, report, pagePath, s.displayLoc)
	if delivered, derr := s.deliver(body); derr != nil || !delivered {
		s.logger.Error("plaud recordings: feed delivery failed",
			"id", f.ID, "delivered", delivered, "error", derr)
	}

	s.logger.Info("plaud recordings: meeting analyzed",
		"id", f.ID, "name", f.Name, "page", pagePath, "projects", strings.Join(related, ","))
	return nil
}

// analyzeMeeting produces the Korean meeting report. Long transcripts are
// map-reduced through the local gist model first (model-roles dogma: the
// synthesis the user reads is the only cloud-eligible call).
func (s *plaudRecordingsService) analyzeMeeting(ctx context.Context, f plaudFile, transcript string, cands []mailanalysis.ProjectCandidate) (string, error) {
	input := transcript
	if utf8.RuneCountInString(transcript) > plaudDirectRunes && s.gist != nil {
		reduced, err := s.reduceTranscript(ctx, transcript)
		if err != nil {
			s.logger.Warn("plaud recordings: chunk reduce failed, truncating instead", "error", err)
			input = truncateRunes(transcript, plaudDirectRunes)
		} else {
			input = reduced
		}
	} else if utf8.RuneCountInString(transcript) > plaudDirectRunes {
		input = truncateRunes(transcript, plaudDirectRunes)
	}

	var candList strings.Builder
	for i, c := range cands {
		if i >= plaudMaxCandidates {
			break
		}
		fmt.Fprintf(&candList, "- %s — %s\n", c.Path, strings.TrimSpace(c.Title+" "+c.Summary))
	}

	// The 업무 topic-knowledge block (company/org/people) is the subtractive
	// slice of the main agent context: the 2026-07-06 correction bake-off
	// showed context is what separates "매/메가/kW" judgment from blind
	// glossary matching, and this block carries it at ~2KB.
	var knowledge string
	if s.topic != nil {
		if t := strings.TrimSpace(s.topic()); t != "" {
			knowledge = "# 배경지식\n\n" + t + "\n\n---\n\n"
		}
	}

	system := knowledge + `당신은 업무 회의록 분석가다. 회의 전사를 읽고 한국어 회의록을 작성한다.
전사는 음성인식 결과라 오인식이 섞여 있다 — 용어·단위·고유명사의 명백한 오인식은 문맥과 배경지식으로
교정해 본문에 반영하라 (발전용량 '메가(MW)'/철골 자재 '매(장)'/생산량 '톤'/소형 설비 'kW'를 구분하고,
프로젝트·인명·지명은 아는 표기를 우선한다).

출력 형식 (마크다운, 이 구조 그대로):
## 요약
- (3~6개 불릿 — 무슨 회의였고 무엇이 진행됐는지)
## 결정사항
- (확정된 결정만. 없으면 "- 없음")
## 액션 아이템
- (담당자/기한이 언급됐으면 함께. 없으면 "- 없음")
## 리스크·미해결
- (걸림돌, 다음 회의로 넘어간 쟁점. 없으면 "- 없음")
## 표기 교정
- (본문에 반영한 대표 교정 5~15개: "원문 → 교정" 꼴. 실제로 표기가 달라진 항목만 —
  원문과 교정문이 같은 항목·"유지/확인" 항목은 넣지 마라. 없으면 "- 없음")

규칙:
- 전사에 있는 내용만 쓴다. 추측은 "(추정)"을 붙인다.
- 발언자 이름이 식별되면 결정/액션에 이름을 남긴다.
- 마지막 줄에 정확히 한 줄: "관련프로젝트: <아래 후보 목록의 path를 쉼표로, 해당 없으면 없음>"
- 관련프로젝트는 후보 목록에 있는 path만 쓸 수 있다.`

	user := fmt.Sprintf("# 회의 정보\n- 제목: %s\n- 일시: %s (KST)\n- 길이: %d분\n\n# 프로젝트 후보 목록\n%s\n# 전사\n%s",
		f.Name, f.StartAt.In(s.displayLoc).Format("2006-01-02 15:04"),
		int(f.Duration.Minutes()), candList.String(), input)

	out, err := s.synthesize(ctx, system, user, plaudSynthesisTokens)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("empty synthesis")
	}
	return out, nil
}

// reduceTranscript map-reduces an over-long transcript: fixed-size rune chunks
// → one gist each (local model) → concatenated gists as the synthesis input.
func (s *plaudRecordingsService) reduceTranscript(ctx context.Context, transcript string) (string, error) {
	runes := []rune(transcript)
	var gists []string
	for start := 0; start < len(runes); start += plaudChunkRunes {
		end := start + plaudChunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunkCtx, cancel := context.WithTimeout(ctx, plaudToolTimeout)
		gist, err := s.gist(chunkCtx,
			"회의 전사 조각을 읽고 진행 내용·결정·액션·수치를 보존한 한국어 요약을 10줄 이내로 써라. 발언자 이름은 유지하라.",
			string(runes[start:end]), plaudChunkMaxTokens)
		cancel()
		if err != nil {
			return "", err
		}
		gists = append(gists, strings.TrimSpace(gist))
	}
	return "(장시간 회의 — 구간별 요약본)\n\n" + strings.Join(gists, "\n\n---\n\n"), nil
}

func (s *plaudRecordingsService) projectCandidates() []mailanalysis.ProjectCandidate {
	if s.candidates == nil {
		return nil
	}
	return s.candidates()
}

// handleToolError separates "not wired yet" (quiet) from auth expiry (loud,
// throttled operator card) from everything else (warn).
func (s *plaudRecordingsService) handleToolError(err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unknown tool"):
		// MCP discovery hasn't registered the tools (env off, npx cold start,
		// or discovery failed) — normal on dev boxes, Debug only.
		s.logger.Debug("plaud recordings: tools not registered, skipping tick")
	case strings.Contains(msg, "401") || strings.Contains(strings.ToLower(msg), "not authenticated") ||
		strings.Contains(strings.ToLower(msg), "unauthorized"):
		s.logger.Error("plaud recordings: MCP auth expired — operator re-login needed", "error", err)
		s.notifyAuthExpiredOnce()
	default:
		s.logger.Warn("plaud recordings: list_files failed", "error", err)
	}
}

// notifyAuthExpiredOnce posts at most one token-expiry card per 24h.
func (s *plaudRecordingsService) notifyAuthExpiredOnce() {
	now := s.now()
	s.mu.Lock()
	last := time.UnixMilli(s.state.LastAuthNotify)
	if now.Sub(last) < plaudAuthNotifyEvery {
		s.mu.Unlock()
		return
	}
	s.state.LastAuthNotify = now.UnixMilli()
	s.mu.Unlock()
	s.saveState()
	if _, err := s.deliver("🎙 Plaud 연동 토큰이 만료됐습니다. 재로그인해 주시면 녹음 분석이 재개됩니다."); err != nil {
		s.logger.Warn("plaud recordings: auth-expiry notice delivery failed", "error", err)
	}
}

// --- parsing ----------------------------------------------------------------

// parsePlaudList decodes plaud_list_files output: {"type":"list","data":[...]}
// with id/name/start_at/duration(ms) rows. Timestamps are naive UTC
// ("2026-07-06T01:05:35", sometimes fractional).
func parsePlaudList(raw string) ([]plaudFile, error) {
	var envelope struct {
		Data []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			StartAt  string  `json:"start_at"`
			Duration float64 `json:"duration"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, fmt.Errorf("list envelope: %w", err)
	}
	out := make([]plaudFile, 0, len(envelope.Data))
	for _, d := range envelope.Data {
		out = append(out, plaudFile{
			ID:       strings.TrimSpace(d.ID),
			Name:     strings.TrimSpace(d.Name),
			StartAt:  parsePlaudTime(d.StartAt),
			Duration: time.Duration(d.Duration) * time.Millisecond,
		})
	}
	return out, nil
}

func parsePlaudTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// parsePlaudTranscript flattens get_transcript output into "speaker: text"
// lines. The payload is a JSON array of segments whose data_content is itself
// a JSON-encoded array of utterances; field names beyond "content" are not
// contractual, so unknown shapes degrade to the raw text (the synthesis model
// tolerates it) rather than failing the recording.
func parsePlaudTranscript(raw string) string {
	var segments []struct {
		DataContent string `json:"data_content"`
	}
	if err := json.Unmarshal([]byte(raw), &segments); err != nil {
		// The MCP tool result may wrap the JSON array in extra text (the
		// production first run archived a raw-JSON excerpt because of this) —
		// retry on the outermost bracketed span before giving up.
		s, e := strings.Index(raw, "["), strings.LastIndex(raw, "]")
		if s < 0 || e <= s || json.Unmarshal([]byte(raw[s:e+1]), &segments) != nil {
			return strings.TrimSpace(raw)
		}
	}
	var b strings.Builder
	for _, seg := range segments {
		if strings.TrimSpace(seg.DataContent) == "" {
			continue
		}
		var utterances []map[string]any
		if err := json.Unmarshal([]byte(seg.DataContent), &utterances); err != nil {
			b.WriteString(strings.TrimSpace(seg.DataContent))
			b.WriteString("\n")
			continue
		}
		for _, u := range utterances {
			content, _ := u["content"].(string)
			if strings.TrimSpace(content) == "" {
				continue
			}
			if sp := utteranceSpeaker(u); sp != "" {
				b.WriteString(sp)
				b.WriteString(": ")
			}
			b.WriteString(strings.TrimSpace(content))
			b.WriteString("\n")
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return strings.TrimSpace(raw)
	}
	return out
}

func utteranceSpeaker(u map[string]any) string {
	for _, key := range []string{"speaker", "speaker_name", "speaker_label", "speaker_id"} {
		switch v := u[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return fmt.Sprintf("화자%d", int(v))
		}
	}
	return ""
}

// splitRelatedProjects pops the trailing "관련프로젝트:" line off the report and
// resolves it against the offered candidates — the model may only link paths
// it was shown (anti-hallucination), capped at plaudRelatedProjectCap.
func splitRelatedProjects(report string, cands []mailanalysis.ProjectCandidate) (string, []string) {
	lines := strings.Split(strings.TrimSpace(report), "\n")
	idx := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-4; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "관련프로젝트:") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return report, nil
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[idx]), "관련프로젝트:"))
	body := strings.TrimSpace(strings.Join(append(append([]string{}, lines[:idx]...), lines[idx+1:]...), "\n"))
	if value == "" || value == "없음" {
		return body, nil
	}
	known := make(map[string]bool, len(cands))
	for _, c := range cands {
		known[strings.TrimSpace(c.Path)] = true
	}
	var related []string
	for _, p := range strings.Split(value, ",") {
		p = strings.TrimSpace(p)
		if p == "" || !known[p] {
			continue
		}
		related = append(related, p)
		if len(related) >= plaudRelatedProjectCap {
			break
		}
	}
	return body, related
}

// --- page / card shaping ----------------------------------------------------

// meetingFilename builds the wiki filename: a Korean-safe slug of the name
// plus an id prefix so retitled recordings stay idempotent by id.
func meetingFilename(f plaudFile) string {
	slug := meetingSlug(f.Name)
	id := f.ID
	if len(id) > 8 {
		id = id[:8]
	}
	if slug == "" {
		return "회의-" + id + ".md"
	}
	return slug + "-" + id + ".md"
}

// meetingSlug keeps unicode letters/digits joined by single hyphens, bounded.
func meetingSlug(name string) string {
	var b strings.Builder
	lastHyphen := true
	runes := 0
	for _, r := range strings.TrimSpace(name) {
		if runes >= 48 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
			runes++
			continue
		}
		if !lastHyphen {
			b.WriteRune('-')
			lastHyphen = true
			runes++
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildMeetingPage(f plaudFile, report string, related []string, transcript string, now time.Time, loc *time.Location) *wiki.Page {
	title := strings.TrimSpace(f.Name)
	if title == "" {
		title = "회의 " + f.ID
	}
	today := now.UTC().Format("2006-01-02")

	var body strings.Builder
	fmt.Fprintf(&body, "> 녹음: %s (KST) · %d분 · Plaud `%s`\n\n",
		f.StartAt.In(loc).Format("2006-01-02 15:04"), int(f.Duration.Minutes()), f.ID)
	body.WriteString(report)
	// A bounded transcript tail keeps the page greppable/quotable without
	// storing a 2-hour transcript in the wiki (the recorder holds the full
	// text; the agent can re-pull via plaud_get_transcript).
	body.WriteString("\n\n## 전사 발췌\n\n```\n")
	body.WriteString(truncateRunes(transcript, 4000))
	body.WriteString("\n```\n")

	return &wiki.Page{
		Meta: wiki.Frontmatter{
			Title:      title,
			Summary:    "회의 녹음 분석 (Plaud)",
			Category:   wikiProjectCategory,
			Tags:       []string{"회의록"},
			Related:    related,
			Created:    today,
			Updated:    today,
			Type:       "log",
			Confidence: "medium",
			Importance: 0.4,
		},
		Body: body.String(),
	}
}

// meetingStatusLine is the dated bullet appended to linked project rep pages.
func meetingStatusLine(f plaudFile, loc *time.Location) string {
	return fmt.Sprintf("회의(녹음): %s (%s, %d분) — 회의록 페이지 참조",
		strings.TrimSpace(f.Name), f.StartAt.In(loc).Format("01-02 15:04"), int(f.Duration.Minutes()))
}

// meetingCardBody is the work-feed card text: the report minus the transcript
// (the card is a read-in-feed artifact; the page carries the excerpt).
func meetingCardBody(f plaudFile, report string, pagePath string, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎙 회의 분석: %s\n%s (KST) · %d분\n\n",
		strings.TrimSpace(f.Name), f.StartAt.In(loc).Format("2006-01-02 15:04"), int(f.Duration.Minutes()))
	b.WriteString(report)
	fmt.Fprintf(&b, "\n\n회의록: %s", pagePath)
	return b.String()
}

// --- state ------------------------------------------------------------------

func (s *plaudRecordingsService) baselined() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Baselined
}

// seedBaseline marks every pre-existing recording seen without analysis.
func (s *plaudRecordingsService) seedBaseline(files []plaudFile, now time.Time) {
	s.mu.Lock()
	for _, f := range files {
		if f.ID != "" {
			s.state.Seen[f.ID] = now.UnixMilli()
		}
	}
	s.state.Baselined = true
	s.mu.Unlock()
	s.saveState()
	s.logger.Info("plaud recordings: baseline seeded", "count", len(files))
}

func (s *plaudRecordingsService) seen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.state.Seen[id]
	return ok
}

func (s *plaudRecordingsService) markSeen(id string, now time.Time) {
	s.mu.Lock()
	s.state.Seen[id] = now.UnixMilli()
	s.mu.Unlock()
	s.saveState()
}

// pruneState drops seen entries past retention — recordings that old have
// fallen out of the list window and can never re-trigger.
func (s *plaudRecordingsService) pruneState(now time.Time) {
	cutoff := now.Add(-plaudStateRetention).UnixMilli()
	s.mu.Lock()
	changed := false
	for k, ms := range s.state.Seen {
		if ms < cutoff {
			delete(s.state.Seen, k)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.saveState()
	}
}

func (s *plaudRecordingsService) loadState() {
	if s.statePath == "" {
		return
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return // missing → fresh state
	}
	var st plaudRecordingsState
	if err := json.Unmarshal(data, &st); err != nil {
		s.logger.Warn("plaud recordings: corrupt state, starting fresh", "error", err)
		return
	}
	if st.Seen == nil {
		st.Seen = map[string]int64{}
	}
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *plaudRecordingsService) saveState() {
	if s.statePath == "" {
		return
	}
	s.mu.Lock()
	data, err := json.MarshalIndent(s.state, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(s.statePath, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		s.logger.Warn("plaud recordings: failed to persist state", "error", err)
	}
}
