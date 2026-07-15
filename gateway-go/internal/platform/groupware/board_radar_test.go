package groupware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBoardRadarIntervalEnvironment(t *testing.T) {
	t.Setenv(boardRadarIntervalMinutesEnv, "17")
	if got := NewBoardRadar(BoardRadarConfig{}).Interval(); got != 17*time.Minute {
		t.Fatalf("interval = %v, want 17m", got)
	}
	t.Setenv(boardRadarIntervalMinutesEnv, "0")
	if got := NewBoardRadar(BoardRadarConfig{}).Interval(); got != DefaultBoardRadarInterval {
		t.Fatalf("invalid interval = %v, want %v", got, DefaultBoardRadarInterval)
	}
	if got := NewBoardRadar(BoardRadarConfig{}).Name(); got != BoardRadarTaskName {
		t.Fatalf("name = %q, want %q", got, BoardRadarTaskName)
	}
}

func TestBoardRadarBaselineNoFloodNewChangeCapAndPruning(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "board-radar.json")
	posts := []BoardSummary{
		boardPost("4", "four"),
		boardPost("2", "two"),
		boardPost("1", "one"),
		boardPost("3", "three"),
	}
	var calls []string
	radar := NewBoardRadar(BoardRadarConfig{
		StatePath:   statePath,
		MaxPerCycle: 2,
		Now:         func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, limit int) ([]BoardSummary, error) {
			if limit != boardRadarListLimit {
				t.Fatalf("limit = %d, want %d", limit, boardRadarListLimit)
			}
			return append([]BoardSummary(nil), posts...), nil
		},
		OnCandidate: func(_ context.Context, post BoardSummary) error {
			calls = append(calls, post.PostID)
			return nil
		},
	})

	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("baseline invoked callbacks: %v", calls)
	}
	state, err := loadBoardRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Initialized || len(state.Posts) != 4 {
		t.Fatalf("baseline state = %+v", state)
	}
	for id, stored := range state.Posts {
		if !stored.Notified {
			t.Fatalf("baseline post %s not marked notified", id)
		}
	}

	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("unchanged cycle invoked callbacks: %v", calls)
	}

	posts = append(posts, boardPost("7", "seven"), boardPost("5", "five"), boardPost("6", "six"))
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"5", "6"}) {
		t.Fatalf("capped calls = %v", calls)
	}
	state, _ = loadBoardRadarState(statePath)
	if state.Posts["7"].Notified {
		t.Fatal("overflow post must remain retryable")
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"5", "6", "7"}) {
		t.Fatalf("overflow retry calls = %v", calls)
	}

	for i := range posts {
		if posts[i].PostID == "5" {
			posts[i].Title = "five changed"
		}
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"5", "6", "7", "5"}) {
		t.Fatalf("changed post calls = %v", calls)
	}

	posts = []BoardSummary{boardPost("5", "five changed")}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ = loadBoardRadarState(statePath)
	if len(state.Posts) != 1 {
		t.Fatalf("pruned state = %+v", state.Posts)
	}
	if _, exists := state.Posts["1"]; exists {
		t.Fatal("post missing from current recent list was not pruned")
	}
}

func TestBoardRadarFailureStaysRetryable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "board-radar.json")
	var posts []BoardSummary
	attempts := 0
	radar := NewBoardRadar(BoardRadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(context.Context, Config, int) ([]BoardSummary, error) {
			return append([]BoardSummary(nil), posts...), nil
		},
		OnCandidate: func(context.Context, BoardSummary) error {
			attempts++
			if attempts == 1 {
				return errors.New("relay failed")
			}
			return nil
		},
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts = []BoardSummary{boardPost("9", "retry")}
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected callback failure")
	}
	state, err := loadBoardRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Posts["9"].Notified {
		t.Fatal("failed callback marked post notified")
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestBoardRadarFirstSuccessfulListCreatesBaseline(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "board-radar.json")
	listAttempts := 0
	callbacks := 0
	radar := NewBoardRadar(BoardRadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(context.Context, Config, int) ([]BoardSummary, error) {
			listAttempts++
			if listAttempts == 1 {
				return nil, errors.New("temporary list failure")
			}
			return []BoardSummary{boardPost("1", "existing")}, nil
		},
		OnCandidate: func(context.Context, BoardSummary) error {
			callbacks++
			return nil
		},
	})
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected initial list failure")
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed list state stat = %v, want missing", err)
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if callbacks != 0 {
		t.Fatalf("first successful baseline callbacks = %d", callbacks)
	}
	state, err := loadBoardRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Initialized || !state.Posts["1"].Notified {
		t.Fatalf("baseline state = %+v", state)
	}
}

func TestBoardRadarOutsideBusinessHoursSkipsList(t *testing.T) {
	listed := false
	radar := NewBoardRadar(BoardRadarConfig{
		StatePath: filepath.Join(t.TempDir(), "board-radar.json"),
		Now: func() time.Time {
			return time.Date(2026, 7, 18, 12, 0, 0, 0, radarKST)
		},
		List: func(context.Context, Config, int) ([]BoardSummary, error) {
			listed = true
			return nil, nil
		},
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if listed {
		t.Fatal("outside business hours called list")
	}
}

func TestBoardRadarStateFileMode0600(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "nested", "board-radar.json")
	radar := NewBoardRadar(BoardRadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List:      func(context.Context, Config, int) ([]BoardSummary, error) { return nil, nil },
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
}

func boardPost(id, title string) BoardSummary {
	return BoardSummary{
		PostID: id, Title: title, Author: "author", Date: "2026-07-16", CategoryID: "42",
	}
}
