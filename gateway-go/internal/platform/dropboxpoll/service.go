package dropboxpoll

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
)

// Notifier delivers a proactive report to the user (mirrors gmailpoll.Notifier).
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// AgentRunner runs an agent turn with the given prompt and returns the agent's
// final text. The agent does the real work (dropbox analyze + wiki tools); this
// service only detects new files and triggers the turn.
type AgentRunner interface {
	RunAgentTurn(ctx context.Context, prompt string) (string, error)
}

// Config configures the Dropbox folder watcher.
type Config struct {
	FolderPath  string // Dropbox folder to watch (default "/Deneb-Inbox")
	IntervalMin int    // poll interval in minutes (default 10)
	MaxPerCycle int    // max files analyzed per cycle (default 10)
	StateDir    string // ~/.deneb
}

// Service is the periodic Dropbox folder watcher.
type Service struct {
	mu       sync.Mutex
	cfg      Config
	log      *slog.Logger
	client   *dropbox.Client
	agent    AgentRunner
	notifier Notifier
	state    *stateStore
}

var _ autonomous.PeriodicTask = (*Service)(nil)

// NewService builds a watcher with sane defaults applied to cfg.
func NewService(cfg Config, logger *slog.Logger) *Service {
	if cfg.IntervalMin <= 0 {
		cfg.IntervalMin = 10
	}
	if cfg.MaxPerCycle <= 0 {
		cfg.MaxPerCycle = 10
	}
	if strings.TrimSpace(cfg.FolderPath) == "" {
		cfg.FolderPath = "/Deneb-Inbox"
	}
	return &Service{cfg: cfg, log: logger, state: newStateStore(cfg.StateDir)}
}

// SetNotifier wires the proactive delivery sink (业务 chat + workfeed).
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetAgent wires the agent-turn runner used for analysis.
func (s *Service) SetAgent(a AgentRunner) { s.agent = a }

func (s *Service) Name() string { return "dropboxpoll" }

func (s *Service) Interval() time.Duration {
	return time.Duration(s.cfg.IntervalMin) * time.Minute
}

// Run executes one watch cycle. Skips outside KST weekday business hours so new
// files that arrive overnight are reported the next morning, not at 3am.
func (s *Service) Run(ctx context.Context) error {
	if !isBusinessHours() {
		return nil
	}
	client, err := s.ensureClient()
	if err != nil {
		// No token yet (user hasn't run deneb-dropbox-auth) — skip quietly.
		s.log.Debug("dropboxpoll: dropbox client unavailable, skipping", "error", err)
		return nil
	}
	return s.poll(ctx, client)
}

func (s *Service) ensureClient() (*dropbox.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	c, err := dropbox.DefaultClient()
	if err != nil {
		return nil, err
	}
	s.client = c
	return c, nil
}

// watchClient is the subset of *dropbox.Client the poll loop needs, extracted
// so tests can substitute a fake.
type watchClient interface {
	LatestCursor(ctx context.Context, path string, recursive bool) (string, error)
	ListChanges(ctx context.Context, cursor string) ([]dropbox.Entry, string, error)
}

func (s *Service) poll(ctx context.Context, client watchClient) error {
	st, err := s.state.Load()
	if err != nil {
		s.log.Warn("dropboxpoll state load failed, starting fresh", "error", err)
		st = &PollState{seenSet: make(map[string]struct{})}
	}

	// First run: capture a cursor "from now" so the existing backlog is not
	// analyzed (no flood of old files on initial enable).
	if st.Cursor == "" {
		cur, err := client.LatestCursor(ctx, s.cfg.FolderPath, false)
		if err != nil {
			return err
		}
		st.Cursor = cur
		st.LastPollAt = time.Now().UnixMilli()
		return s.state.Save(st)
	}

	entries, newCursor, err := client.ListChanges(ctx, st.Cursor)
	if errors.Is(err, dropbox.ErrCursorReset) {
		// Cursor expired (long idle). Re-seed from the current folder state and
		// skip this cycle; the next cycle diffs from the fresh cursor instead of
		// wedging permanently on the dead cursor.
		s.log.Warn("dropboxpoll cursor reset — re-seeding from latest")
		cur, e := client.LatestCursor(ctx, s.cfg.FolderPath, false)
		if e != nil {
			return e
		}
		st.Cursor = cur
		st.LastPollAt = time.Now().UnixMilli()
		return s.state.Save(st)
	}
	if err != nil {
		return err
	}

	var fresh []dropbox.Entry
	for _, e := range entries {
		if e.ID != "" && !st.hasSeen(e.ID) {
			fresh = append(fresh, e)
		}
	}

	if len(fresh) == 0 {
		st.Cursor = newCursor
		st.LastPollAt = time.Now().UnixMilli()
		return s.state.Save(st)
	}

	// Cap per cycle to bound the trigger prompt. When capped, keep the old
	// cursor so the remainder is picked up next cycle (ListChanges from a cursor
	// is idempotent; seenIDs prevent re-processing what we did handle).
	truncated := false
	if len(fresh) > s.cfg.MaxPerCycle {
		s.log.Warn("dropboxpoll: change burst exceeds MaxPerCycle, processing in batches",
			"total", len(fresh), "perCycle", s.cfg.MaxPerCycle)
		fresh = fresh[:s.cfg.MaxPerCycle]
		truncated = true
	}

	if s.agent == nil {
		s.log.Warn("dropboxpoll: no agent runner wired, skipping analysis")
		return nil
	}
	summary, err := s.agent.RunAgentTurn(ctx, s.buildPrompt(fresh))
	if err != nil {
		// Keep cursor/seen unchanged → retry the same changes next cycle.
		s.log.Error("dropboxpoll agent turn failed", "error", err)
		return err
	}
	if s.notifier != nil && strings.TrimSpace(summary) != "" {
		if err := s.notifier.Notify(ctx, summary); err != nil {
			// Don't mark seen / advance cursor — retry next cycle so the user
			// isn't silently never told these files arrived.
			s.log.Error("dropboxpoll notify failed — will retry next cycle", "error", err)
			return err
		}
	}

	for _, e := range fresh {
		st.markSeen(e.ID)
	}
	if !truncated {
		st.Cursor = newCursor // advance only when fully drained this cycle
	}
	st.LastPollAt = time.Now().UnixMilli()
	return s.state.Save(st)
}

// buildPrompt instructs the agent to analyze the new files (dropbox analyze),
// reflect them into the wiki knowledge graph when relevant, and summarize for
// the user — or stay silent when nothing is worth reporting.
func (s *Service) buildPrompt(files []dropbox.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Dropbox 감시 폴더 `%s`에 새 파일 %d개가 도착했습니다:\n", s.cfg.FolderPath, len(files))
	for _, e := range files {
		path := e.PathDisplay
		if path == "" {
			path = e.Name
		}
		fmt.Fprintf(&b, "- %s (`%s`)\n", e.Name, path)
	}
	b.WriteString("\n각 파일을 dropbox 도구의 analyze 액션(path=위 경로)으로 분석하세요. ")
	b.WriteString("문서 내용이 특정 거래처/프로젝트와 관련되면 wiki 도구로 해당 페이지에 핵심 요약을 추가하세요. ")
	b.WriteString("마지막으로 사용자에게 보고할 핵심을 1~2문단으로 정리해 주세요. ")
	b.WriteString("업무상 의미 없는 파일(임시·중복·개인 파일)뿐이면 NO_REPLY만 출력하세요.")
	return b.String()
}

// isBusinessHours reports whether now is within KST weekday business hours
// (09:00~19:00) — same gate as gmailpoll, to avoid off-hours notifications.
func isBusinessHours() bool {
	kst := time.FixedZone("KST", 9*60*60)
	now := time.Now().In(kst)
	if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	hour := now.Hour()
	return hour >= 9 && hour < 19
}
