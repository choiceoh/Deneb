package groupware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	BoardRadarTaskName           = "groupware-board-radar"
	DefaultBoardRadarInterval    = 30 * time.Minute
	DefaultBoardRadarMaxPerCycle = 3
	boardRadarListLimit          = 50
	boardRadarIntervalMinutesEnv = "DENEB_GROUPWARE_BOARD_RADAR_INTERVAL_MINUTES"
)

// BoardRadarListFunc is the structured recent-board boundary used by BoardRadar.
type BoardRadarListFunc func(context.Context, Config, int) ([]BoardSummary, error)

// BoardRadarConfig supplies the reader, durable state, callback, and test seams.
type BoardRadarConfig struct {
	Reader      Config
	StatePath   string
	Interval    time.Duration
	MaxPerCycle int
	List        BoardRadarListFunc
	Now         func() time.Time
	OnCandidate func(context.Context, BoardSummary) error
}

// BoardRadar deterministically diffs recent notices and evaluates only new,
// changed, or previously failed posts.
type BoardRadar struct {
	reader      Config
	statePath   string
	interval    time.Duration
	maxPerCycle int
	list        BoardRadarListFunc
	now         func() time.Time
	onCandidate func(context.Context, BoardSummary) error
}

var _ autonomous.PeriodicTask = (*BoardRadar)(nil)

type boardRadarState struct {
	Initialized bool                           `json:"initialized"`
	Posts       map[string]boardRadarPostState `json:"posts"`
	LastPollAt  int64                          `json:"lastPollAt,omitempty"`
}

type boardRadarPostState struct {
	Fingerprint string `json:"fingerprint"`
	Notified    bool   `json:"notified"`
	LastSeenAt  int64  `json:"lastSeenAt"`
}

// NewBoardRadar constructs a serial periodic board radar.
func NewBoardRadar(cfg BoardRadarConfig) *BoardRadar {
	interval := cfg.Interval
	if interval <= 0 {
		interval = boardRadarIntervalFromEnv()
	}
	maxPerCycle := cfg.MaxPerCycle
	if maxPerCycle <= 0 {
		maxPerCycle = DefaultBoardRadarMaxPerCycle
	}
	list := cfg.List
	if list == nil {
		list = ListBoardPosts
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &BoardRadar{
		reader:      cfg.Reader,
		statePath:   strings.TrimSpace(cfg.StatePath),
		interval:    interval,
		maxPerCycle: maxPerCycle,
		list:        list,
		now:         now,
		onCandidate: cfg.OnCandidate,
	}
}

func (r *BoardRadar) Name() string { return BoardRadarTaskName }

func (r *BoardRadar) Interval() time.Duration { return r.interval }

// Run polls one recent-board snapshot. The autonomous service serializes task
// cycles, so BoardRadar intentionally holds no mutex across callbacks.
func (r *BoardRadar) Run(ctx context.Context) error {
	now := r.now()
	if !IsRadarBusinessHours(now) {
		return nil
	}
	if r.statePath == "" {
		return errors.New("groupware board radar state path is required")
	}

	state, err := loadBoardRadarState(r.statePath)
	if err != nil {
		return err
	}
	posts, err := r.list(ctx, r.reader, boardRadarListLimit)
	if err != nil {
		return fmt.Errorf("list groupware board posts: %w", err)
	}
	sortBoardSummaries(posts)

	nowMs := now.UnixMilli()
	next := make(map[string]boardRadarPostState, len(posts))
	current := make(map[string]BoardSummary, len(posts))
	for _, post := range posts {
		id := strings.TrimSpace(post.PostID)
		if id == "" {
			continue
		}
		if _, duplicate := current[id]; duplicate {
			continue
		}
		current[id] = post
		fingerprint := boardPostFingerprint(post)
		stored, exists := state.Posts[id]
		if !exists || stored.Fingerprint != fingerprint {
			stored = boardRadarPostState{Fingerprint: fingerprint}
		}
		stored.LastSeenAt = nowMs
		next[id] = stored
	}
	state.Posts = next
	state.LastPollAt = nowMs

	// The first successful list is a baseline, not a backlog: every current post
	// is marked handled without reading bodies or invoking the importance model.
	if !state.Initialized {
		state.Initialized = true
		for id, stored := range state.Posts {
			stored.Notified = true
			state.Posts[id] = stored
		}
		return saveBoardRadarState(r.statePath, state)
	}

	candidates := make([]BoardSummary, 0, len(current))
	for id, post := range current {
		if !state.Posts[id].Notified {
			candidates = append(candidates, post)
		}
	}
	sortBoardSummaries(candidates)

	var runErrs []error
	for i, post := range candidates {
		if i >= r.maxPerCycle {
			break
		}
		if r.onCandidate == nil {
			runErrs = append(runErrs, fmt.Errorf("board post %s: callback unavailable", post.PostID))
			continue
		}
		if err := r.onCandidate(ctx, post); err != nil {
			runErrs = append(runErrs, fmt.Errorf("board post %s: %w", post.PostID, err))
			continue
		}
		id := strings.TrimSpace(post.PostID)
		stored := state.Posts[id]
		stored.Notified = true
		state.Posts[id] = stored
	}
	if err := saveBoardRadarState(r.statePath, state); err != nil {
		runErrs = append(runErrs, err)
	}
	return errors.Join(runErrs...)
}

func boardRadarIntervalFromEnv() time.Duration {
	minutes, err := strconv.Atoi(strings.TrimSpace(os.Getenv(boardRadarIntervalMinutesEnv)))
	if err == nil && minutes > 0 {
		return time.Duration(minutes) * time.Minute
	}
	return DefaultBoardRadarInterval
}

func sortBoardSummaries(posts []BoardSummary) {
	sort.SliceStable(posts, func(i, j int) bool {
		return strings.TrimSpace(posts[i].PostID) < strings.TrimSpace(posts[j].PostID)
	})
}

func boardPostFingerprint(post BoardSummary) string {
	raw := strings.Join([]string{
		strings.TrimSpace(post.PostID),
		strings.TrimSpace(post.Title),
		strings.TrimSpace(post.Author),
		strings.TrimSpace(post.Date),
		strings.TrimSpace(post.CategoryID),
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func loadBoardRadarState(path string) (boardRadarState, error) {
	state := boardRadarState{Posts: make(map[string]boardRadarPostState)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return boardRadarState{}, fmt.Errorf("read groupware board radar state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return boardRadarState{}, fmt.Errorf("parse groupware board radar state: %w", err)
	}
	if state.Posts == nil {
		state.Posts = make(map[string]boardRadarPostState)
	}
	return state, nil
}

func saveBoardRadarState(path string, state boardRadarState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal groupware board radar state: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.WriteFile(path, data, &atomicfile.Options{
		Perm:    0o600,
		DirPerm: 0o700,
		Fsync:   true,
	}); err != nil {
		return fmt.Errorf("write groupware board radar state: %w", err)
	}
	return nil
}
