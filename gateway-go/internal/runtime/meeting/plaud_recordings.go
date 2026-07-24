// plaud_recordings.go — autonomous meeting-recording analysis (Plaud MCP).
//
// The mail pipeline gives every inbound mail a full analysis pass; recordings
// deserved the same but only had a pull path (chat tools, on request) plus
// whatever Plaud's AutoFlow mails happened to deliver through the fragile
// mail edge. This service closes that gap from the recorder side: it polls
// the Plaud MCP tools the gateway already registers (mcp_external_tools.go),
// detects new recordings, pulls the speaker-attributed transcript, runs a
// meeting-shaped synthesis (main role — the user reads this; analysis role
// was retired 2026-07-07), and lands
// the result everywhere the flywheel expects:
//
//   - a 회의록/ wiki page (per linked project, else the category bucket),
//   - one dated status bullet on each linked project 대표페이지,
//   - a work-feed card (SourceMeetingReport, feed-only doctrine).
//
// Ordering with AutoFlow mails: both paths may cover the same meeting. The
// mail path stays the auth-fallback; this service is the deep, meeting-shaped
// one. Work-feed near-dup across mail_report↔meeting_report collapses the
// weaker second card (workfeed.AppendIfNew).
//
// First run seeds a baseline: every recording that exists before the service
// first ticks is marked seen WITHOUT analysis — months of backlog (and
// Plaud's onboarding samples) must not burn analysis tokens on deploy day.
package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	plaudPollInterval = 15 * time.Minute
	plaudStateFile    = "plaud-recordings-state.json"
	// Tool names as registered by mcp_external_tools.go (<server>_<tool>).
	plaudListTool       = "plaud_list_files"
	plaudTranscriptTool = "plaud_get_transcript"
	plaudToolTimeout    = 3 * time.Minute
	// plaudListWindowDays bounds each list call. The recorder can sit unsynced
	// for days (2026-07-20: a 07-14 recording synced 6 days late and survived
	// the old 7-day window by one day) — cover a vacation-length gap. Must stay
	// well under plaudStateRetention or pruned entries could re-trigger.
	plaudListWindowDays = 30
	// plaudListPageSize is requested per list call; plaudListPageFloor is the
	// server's default page cap — a page with at least that many rows may be
	// truncated regardless of the requested size (the tool docs say page params
	// are ignored when date filters are set, but the observed cap semantics are
	// unspecified), so keep paging until a page comes back smaller or adds
	// nothing new. plaudListMaxPages bounds the loop either way.
	plaudListPageSize  = 100
	plaudListPageFloor = 20
	plaudListMaxPages  = 5
	// plaudMaxPerTick bounds one tick's LLM work; the unprocessed rest stays
	// unseen and comes around next tick.
	plaudMaxPerTick = 3
	// plaudMinDuration skips accidental taps; short voice memos still pass.
	plaudMinDuration = 2 * time.Minute
	// plaudMinTranscriptRunes: below this the ASR heard nothing worth a
	// synthesis call — either a silent recording or a transcript that isn't
	// ready yet; the retry budget below decides which.
	plaudMinTranscriptRunes = 200
	// plaudTranscriptWaitTicks: Plaud lists a recording as soon as the device
	// syncs, but cloud transcription can lag behind by minutes (2026-07-20:
	// two real meetings were permanently skipped as "silent" in that gap). An
	// empty transcript is retried this many ticks (~1h) before the recording
	// is accepted as genuinely silent.
	plaudTranscriptWaitTicks = 4
	// Transcripts under the direct limit go to the synthesis model whole;
	// longer ones are map-reduced (chunk gists via the RoleTiny stage-1 model,
	// then one synthesis over the gists) so a 2-hour workshop cannot blow the
	// main-role (RoleMain) model's context or cost.
	plaudDirectRunes = 28000
	plaudChunkRunes  = 12000
	// plaudChunkMaxTokens: a "10줄 이내" Korean gist overran 500 on 2026-07-10
	// and the whole reduce fell back to truncation; 800 gives headroom.
	plaudChunkMaxTokens = 800
	// plaudChunkFallbackRunes bounds the raw excerpt substituted for a chunk
	// whose gist call failed (coverage beats elegance for a lost chunk).
	plaudChunkFallbackRunes = 2000
	// plaudSynthesisTokens covers the report plus the 표기 교정 appendix; the
	// first-tick incident showed 1600 is tight even for the report alone.
	plaudSynthesisTokens   = 2800
	plaudMaxCandidates     = 40 // project candidates offered to the model
	plaudStateRetention    = 180 * 24 * time.Hour
	plaudAuthNotifyEvery   = 24 * time.Hour
	plaudRelatedProjectCap = 3
	meetingWikiCategory    = "프로젝트"
)

// plaudRecordingsDisableEnv kills the service without a deploy (incident lever).
const plaudRecordingsDisableEnv = "DENEB_PLAUD_RECORDINGS_DISABLE"

// errTranscriptNotReady flags a recording whose transcript came back (near)
// empty — transcription may simply not have finished yet, so the tick loop
// retries within plaudTranscriptWaitTicks instead of marking it seen.
var errTranscriptNotReady = errors.New("transcript empty or not ready")

// PlaudDisableEnv disables autonomous Plaud recording ingestion when set to 1.
const PlaudDisableEnv = plaudRecordingsDisableEnv

// PlaudStateFile is the durable recording-ingestion cursor filename.
const PlaudStateFile = plaudStateFile

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
	Failures             map[string]int `json:"failures,omitempty"`
	LastAuthNotify       int64          `json:"lastAuthNotify,omitempty"`
	LastQuarantineNotify int64          `json:"lastQuarantineNotify,omitempty"`
}

// plaudRecordingsService polls Plaud via the chat ToolRegistry and lands
// meeting analyses in the wiki + work feed. Mirrors meetingHarvestService's
// lifecycle (nil-safe start, safego goroutine, ShutdownCtx-bound).
type plaudRecordingsService struct {
	execTool func(ctx context.Context, name string, args json.RawMessage) (string, error)
	// synthesize runs the main-role model (RoleMain; user-facing report; cloud OK).
	synthesize func(ctx context.Context, system, user string, maxTokens int) (string, error)
	// gist runs the RoleTiny stage-1 model for map-reduce chunk summaries.
	gist       func(ctx context.Context, system, user string, maxTokens int) (string, error)
	candidates func() []mailanalysis.ProjectCandidate
	// topic returns the 업무 topic-knowledge block (company, org, key people)
	// injected into the synthesis prompt — situational context. Empty when
	// topics are unconfigured; nil skips injection entirely.
	topic func() string
	// glossary returns the full topics/plaud-glossary.md body (tests / override).
	// When nil, LoadPlaudGlossary(topicsDir) is used. The body is sliced per meeting.
	glossary func() string
	// correctionPrompt returns topics/plaud-correction.md (ASR correction
	// instructions). Nil falls back to defaultPlaudCorrectionPrompt.
	correctionPrompt func() string
	// topicsDir is the workspace topics/ path for glossary / do-not-correct /
	// auto-promotion. Empty disables file I/O (tests may still inject glossary).
	topicsDir string
	// projectEntities loads wiki-backed people/places/orgs for mentioned
	// project 대표페이지 paths. Nil skips the project-entity block.
	projectEntities func(paths []string) []ProjectEntityFacts
	listCalendar    func(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
	priorMeeting    func(projectPath string) (title, body string)
	writePage       func(relPath string, page *wiki.Page) error
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

// PlaudService ingests new Plaud recordings into meeting reports, wiki pages,
// and the native work feed.
type PlaudService = plaudRecordingsService

func newPlaudRecordingsService(
	execTool func(ctx context.Context, name string, args json.RawMessage) (string, error),
	synthesize func(ctx context.Context, system, user string, maxTokens int) (string, error),
	gist func(ctx context.Context, system, user string, maxTokens int) (string, error),
	candidates func() []mailanalysis.ProjectCandidate,
	topic func() string,
	glossary func() string,
	correctionPrompt func() string,
	topicsDir string,
	projectEntities func(paths []string) []ProjectEntityFacts,
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
		execTool:         execTool,
		synthesize:       synthesize,
		gist:             gist,
		candidates:       candidates,
		topic:            topic,
		glossary:         glossary,
		correctionPrompt: correctionPrompt,
		topicsDir:        strings.TrimSpace(topicsDir),
		projectEntities:  projectEntities,
		writePage:        writePage,
		appendStatus:     appendStatus,
		deliver:          deliver,
		logger:           logger,
		statePath:        statePath,
		displayLoc:       loc,
		now:              time.Now,
		state:            plaudRecordingsState{Version: 1, Seen: map[string]int64{}},
	}
	s.loadState()
	return s
}

// NewPlaudService constructs the autonomous Plaud recording ingestion worker.
func NewPlaudService(
	execTool func(ctx context.Context, name string, args json.RawMessage) (string, error),
	synthesize func(ctx context.Context, system, user string, maxTokens int) (string, error),
	gist func(ctx context.Context, system, user string, maxTokens int) (string, error),
	candidates func() []mailanalysis.ProjectCandidate,
	topic func() string,
	glossary func() string,
	correctionPrompt func() string,
	topicsDir string,
	projectEntities func(paths []string) []ProjectEntityFacts,
	writePage func(relPath string, page *wiki.Page) error,
	appendStatus func(projectPath, line, ref string, now time.Time) error,
	deliver func(text string) (bool, error),
	statePath string,
	logger *slog.Logger,
) *PlaudService {
	return newPlaudRecordingsService(execTool, synthesize, gist, candidates, topic,
		glossary, correctionPrompt, topicsDir, projectEntities, writePage, appendStatus, deliver, statePath, logger)
}

// SetCalendarLister wires calendar overlap matching for Plaud recordings.
func (s *plaudRecordingsService) SetCalendarLister(fn func(ctx context.Context, from, to time.Time) ([]calendar.Event, error)) {
	if s != nil {
		s.listCalendar = fn
	}
}

// SetPriorMeetingLoader wires prior 회의록 continuity for synthesis.
func (s *plaudRecordingsService) SetPriorMeetingLoader(fn func(projectPath string) (title, body string)) {
	if s != nil {
		s.priorMeeting = fn
	}
}

// Start launches the periodic Plaud-recording ingest loop until ctx is cancelled.
func (s *plaudRecordingsService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	safego.GoWithSlog(s.logger, "plaud-recordings", func() {
		ticker := time.NewTicker(plaudPollInterval)
		defer ticker.Stop()
		// Delayed first pass: MCP discovery is async; full poll left new
		// recordings idle ~15–30m after deploy.
		select {
		case <-ctx.Done():
			s.logger.Debug("plaud recordings service stopped")
			return
		case <-time.After(plaudFirstTickDelay):
			s.tick(ctx)
		}
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
	files, err := s.listRecordings(ctx, now)
	if err != nil {
		s.handleToolError(err)
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
			n := s.bumpFailure(f.ID)
			if errors.Is(err, errTranscriptNotReady) {
				// Not an operator-card failure: the recording is listed but its
				// cloud transcript may still be minutes away. Give up quietly
				// once the wait budget says it is a genuinely silent recording.
				if n >= plaudTranscriptWaitTicks {
					s.logger.Info("plaud recordings: transcript still empty, treating as silent recording",
						"id", f.ID, "name", f.Name, "attempts", n)
					s.markSeen(f.ID, s.now())
					s.clearFailure(f.ID)
				} else {
					s.logger.Info("plaud recordings: transcript not ready, will retry",
						"id", f.ID, "name", f.Name, "attempts", n)
				}
				continue
			}
			s.logger.Error("plaud recordings: analysis failed (will retry)",
				"id", f.ID, "name", f.Name, "failures", n, "error", err)
			if n >= plaudMaxFailures {
				s.markSeen(f.ID, s.now())
				s.clearFailure(f.ID)
				s.notifyQuarantineOnce(f)
			}
			continue
		}
		s.clearFailure(f.ID)
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
		return fmt.Errorf("%w (%d runes)", errTranscriptNotReady, utf8.RuneCountInString(transcript))
	}

	cands := s.projectCandidates()
	report, err := s.analyzeMeeting(ctx, f, transcript, cands)
	if err != nil {
		return fmt.Errorf("synthesis: %w", err)
	}
	report, related := relatedProjectsOrFallback(report, cands, f.Name, transcript)

	if s.topicsDir != "" {
		if n, perr := PromotePlaudCorrections(s.topicsDir, ExtractReportCorrectionPairs(report), f.ID); perr != nil {
			s.logger.Warn("plaud recordings: glossary promote failed", "id", f.ID, "error", perr)
		} else if n > 0 {
			s.logger.Info("plaud recordings: glossary promoted", "id", f.ID, "pairs", n)
		}
	}

	// Wiki page — one per recording, project slot when linked.
	project := ""
	if len(related) > 0 {
		if name, ok := wiki.ProjectNameOf(related[0]); ok {
			project = name
		}
	}
	spill := writeTranscriptSpill(s.statePath, f.ID, transcript)
	var calEv *calendar.Event
	if s.listCalendar != nil && !f.StartAt.IsZero() {
		from, to := f.StartAt.Add(-plaudCalendarMatchWindow), f.StartAt.Add(f.Duration).Add(plaudCalendarMatchWindow)
		if evs, cerr := s.listCalendar(ctx, from, to); cerr != nil {
			s.logger.Debug("plaud recordings: calendar list failed", "error", cerr)
		} else {
			calEv = matchCalendarEvent(f, evs)
		}
	}
	pagePath := wiki.MeetingPagePath(project, meetingFilename(f))
	page := buildMeetingPage(f, report, related, transcript, now, s.displayLoc)
	if due := extractEarliestDue(report); due != "" {
		page.Meta.Due = due
	}
	if spill != "" {
		page.Body = strings.TrimRight(page.Body, "\n") + "\n\n> 전문 전사 spill: `" + spill + "`\n"
	}
	if calEv != nil {
		page.Body = strings.TrimRight(page.Body, "\n") + fmt.Sprintf(
			"\n\n> 캘린더 연결: %s (`%s`)\n", strings.TrimSpace(calEv.Summary), calEv.ID,
		)
		if calEv.ID != "" {
			page.Meta.Related = append(page.Meta.Related, "calendar:"+calEv.ID)
		}
	}
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
		reduced, err := s.reduceTranscript(ctx, transcript, s.meetingHeader(f))
		if err != nil {
			s.logger.Warn("plaud recordings: chunk reduce failed, truncating instead", "error", err)
			input = textutil.TruncateRunes(transcript, plaudDirectRunes, "")
		} else {
			input = reduced
		}
	} else if utf8.RuneCountInString(transcript) > plaudDirectRunes {
		input = textutil.TruncateRunes(transcript, plaudDirectRunes, "")
	}

	var candList strings.Builder
	for i, c := range cands {
		if i >= plaudMaxCandidates {
			break
		}
		fmt.Fprintf(&candList, "- %s — %s\n", c.Path, strings.TrimSpace(c.Title+" "+c.Summary))
	}

	// Background (업무 topic) + mentioned-project entities + sliced glossary +
	// correction prompt. Project entities come from wiki 대표페이지 when the
	// recording/transcript mentions a project name.
	var b strings.Builder
	if s.topic != nil {
		if t := strings.TrimSpace(s.topic()); t != "" {
			b.WriteString("# 배경지식\n\n")
			b.WriteString(t)
			b.WriteString("\n\n---\n\n")
		}
	}
	mentioned := RankMentionedProjects(f.Name, transcript, cands, plaudMaxMentionedProjects)
	if s.priorMeeting != nil && len(mentioned) > 0 {
		if title, body := s.priorMeeting(mentioned[0].Path); strings.TrimSpace(body) != "" || strings.TrimSpace(title) != "" {
			if blk := formatPriorMeetingBlock(title, body); blk != "" {
				b.WriteString(blk)
				b.WriteString("\n\n---\n\n")
			}
		}
	}
	var entityFacts []ProjectEntityFacts
	if s.projectEntities != nil && len(mentioned) > 0 {
		paths := make([]string, 0, len(mentioned))
		for _, m := range mentioned {
			paths = append(paths, m.Path)
		}
		entityFacts = s.projectEntities(paths)
		if block := FormatProjectEntityBlock(entityFacts); block != "" {
			b.WriteString("# 프로젝트 연관 고유명\n\n")
			b.WriteString(block)
			b.WriteString("\n\n---\n\n")
		}
	}
	fullGlossary := ""
	if s.glossary != nil {
		fullGlossary = strings.TrimSpace(s.glossary())
	} else if s.topicsDir != "" {
		fullGlossary = LoadPlaudGlossary(s.topicsDir)
	}
	doNot := ""
	if s.topicsDir != "" {
		doNot = LoadPlaudDoNotCorrect(s.topicsDir)
	}
	sliceCands := mentioned
	if len(sliceCands) == 0 {
		sliceCands = cands
	}
	if sliced := SlicePlaudGlossary(fullGlossary, doNot, GlossaryHints{
		RecordingName: f.Name,
		Candidates:    sliceCands,
		ExtraTokens:   EntityHintTokens(entityFacts),
	}); sliced != "" {
		b.WriteString("# 용어집 (회의 슬라이스)\n\n")
		b.WriteString(sliced)
		b.WriteString("\n\n---\n\n")
	}
	correction := defaultPlaudCorrectionPrompt
	if s.correctionPrompt != nil {
		if c := strings.TrimSpace(s.correctionPrompt()); c != "" {
			correction = c
		}
	} else if s.topicsDir != "" {
		correction = LoadPlaudCorrectionPrompt(s.topicsDir)
	}
	b.WriteString("# 교정 지침\n\n")
	b.WriteString(correction)
	b.WriteString("\n\n---\n\n")
	b.WriteString(plaudMeetingReportPrompt)

	system := b.String()

	user := fmt.Sprintf("%s\n# 프로젝트 후보 목록\n%s\n# 전사\n%s",
		s.meetingHeader(f), candList.String(), input)

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

// meetingHeader renders the shared "# 회의 정보" block (title/date/length). Used
// verbatim by the synthesis prompt and prepended to every reduce chunk so an
// isolated chunk still knows which meeting it belongs to.
func (s *plaudRecordingsService) meetingHeader(f plaudFile) string {
	return fmt.Sprintf("# 회의 정보\n- 제목: %s\n- 일시: %s (KST)\n- 길이: %d분\n",
		f.Name, f.StartAt.In(s.displayLoc).Format("2006-01-02 15:04"), int(f.Duration.Minutes()))
}

// reduceTranscript map-reduces an over-long transcript: chunks → one gist each
// (local model) → concatenated gists as the synthesis input.
//
// Each chunk carries the meeting header (title/date) and its position, because a
// chunk from the middle of a 2-hour meeting otherwise reaches the gist model with
// no idea what meeting it is, who the participants are, or what came before — the
// model then hedges or mislabels speakers, and those gists are all the synthesis
// pass gets to see. Chunk boundaries also snap to a line break so an utterance
// isn't sliced mid-sentence (both are the "prepend the thread topic / keep the
// unit whole" lesson from Cerebras' knowledge-base write-up).
func (s *plaudRecordingsService) reduceTranscript(ctx context.Context, transcript, meetingHeader string) (string, error) {
	chunks := splitTranscriptChunks([]rune(transcript), plaudChunkRunes)
	gists := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		chunkCtx, cancel := context.WithTimeout(ctx, plaudToolTimeout)
		user := fmt.Sprintf("%s- 구간: %d/%d\n\n# 전사 구간\n%s", meetingHeader, i+1, len(chunks), chunk)
		gist, err := s.gist(chunkCtx,
			"회의 전사 조각을 읽고 진행 내용·결정·액션·수치를 보존한 한국어 요약을 10줄 이내로 써라. 발언자 이름은 유지하라. "+
				"주어진 회의 정보(제목·일시)는 맥락 파악에만 쓰고 요약에 그대로 옮기지 마라.",
			user, plaudChunkMaxTokens)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return "", err
			}
			// One bad chunk must not sink the whole reduce — the old
			// all-or-nothing fallback cut the meeting tail off (2026-07-10,
			// gist overran max_tokens). Substitute a raw excerpt instead.
			s.logger.Warn("plaud recordings: chunk gist failed, using raw excerpt",
				"chunk", i, "error", err)
			gist = textutil.TruncateRunes(chunk, plaudChunkFallbackRunes, "")
		}
		gists = append(gists, strings.TrimSpace(gist))
	}
	return "(장시간 회의 — 구간별 요약본)\n\n" + strings.Join(gists, "\n\n---\n\n"), nil
}

// splitTranscriptChunks slices runes into ~size chunks, snapping each cut back to
// the last line break in the final 20% of the window so a speaker's utterance
// stays whole. A chunk with no usable break (one giant unbroken line) falls back
// to the hard cut so progress is always made.
func splitTranscriptChunks(runes []rune, size int) []string {
	if size <= 0 || len(runes) == 0 {
		return []string{string(runes)}
	}
	var out []string
	for start := 0; start < len(runes); {
		end := start + size
		if end >= len(runes) {
			out = append(out, string(runes[start:]))
			break
		}
		// Look back over the tail of the window for an utterance boundary.
		cut := -1
		for i := end - 1; i > end-size/5 && i > start; i-- {
			if runes[i] == '\n' {
				cut = i + 1 // keep the newline with the earlier chunk
				break
			}
		}
		if cut > start {
			end = cut
		}
		out = append(out, string(runes[start:end]))
		start = end
	}
	return out
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

// listRecordings pulls the recent-window recording list, paging defensively:
// the tool may honor page params, cap a filtered response at its default page
// size, or ignore paging entirely — dedup by ID makes all three converge.
func (s *plaudRecordingsService) listRecordings(ctx context.Context, now time.Time) ([]plaudFile, error) {
	var all []plaudFile
	byID := map[string]bool{}
	for page := 1; page <= plaudListMaxPages; page++ {
		out, err := s.listPage(ctx, now, page)
		var files []plaudFile
		if err == nil {
			files, err = parsePlaudList(out)
			if err != nil {
				err = fmt.Errorf("list parse: %w", err)
			}
		}
		if err != nil {
			if page > 1 {
				// A later page failing must not drop the rows already fetched.
				s.logger.Warn("plaud recordings: list page failed, using partial list",
					"page", page, "error", err)
				break
			}
			return nil, err
		}
		added := 0
		for _, f := range files {
			if f.ID == "" || byID[f.ID] {
				continue
			}
			byID[f.ID] = true
			all = append(all, f)
			added++
		}
		if added == 0 || len(files) < plaudListPageFloor {
			break
		}
	}
	return all, nil
}

// listPage fetches one page, keeping the alt-tool fallback from the
// pre-pagination era (the alt tool tolerates unknown page params).
func (s *plaudRecordingsService) listPage(ctx context.Context, now time.Time, page int) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, plaudToolTimeout)
	defer cancel()
	args, _ := json.Marshal(map[string]any{
		"date_from": now.UTC().AddDate(0, 0, -plaudListWindowDays).Format("2006-01-02"),
		"date_to":   now.UTC().Format("2006-01-02"),
		"page":      page,
		"page_size": plaudListPageSize,
	})
	out, err := s.execTool(callCtx, plaudListTool, args)
	if err == nil {
		return out, nil
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		return "", err
	}
	out, err2 := s.execTool(callCtx, plaudListToolAlt, args)
	if err2 != nil {
		return "", err
	}
	s.logger.Warn("plaud recordings: list_files missing; using search_recordings")
	return out, nil
}

func (s *plaudRecordingsService) bumpFailure(id string) int {
	s.mu.Lock()
	if s.state.Failures == nil {
		s.state.Failures = map[string]int{}
	}
	s.state.Failures[id]++
	n := s.state.Failures[id]
	s.mu.Unlock()
	s.saveState()
	return n
}

func (s *plaudRecordingsService) clearFailure(id string) {
	s.mu.Lock()
	if s.state.Failures != nil {
		delete(s.state.Failures, id)
	}
	s.mu.Unlock()
	s.saveState()
}

func (s *plaudRecordingsService) notifyQuarantineOnce(f plaudFile) {
	now := s.now()
	s.mu.Lock()
	last := time.UnixMilli(s.state.LastQuarantineNotify)
	if now.Sub(last) < plaudAuthNotifyEvery {
		s.mu.Unlock()
		return
	}
	s.state.LastQuarantineNotify = now.UnixMilli()
	s.mu.Unlock()
	s.saveState()
	msg := fmt.Sprintf("🎙 Plaud 녹음 분석이 %d회 실패해 건너뜁니다: %s (`%s`)", plaudMaxFailures, strings.TrimSpace(f.Name), f.ID)
	if _, err := s.deliver(msg); err != nil {
		s.logger.Warn("plaud recordings: quarantine notice delivery failed", "error", err)
	}
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
	if st.Failures == nil {
		st.Failures = map[string]int{}
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
