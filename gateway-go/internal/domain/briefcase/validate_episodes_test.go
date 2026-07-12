package briefcase

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateEpisodesPreservesStepAndInvariantProblemOrder(t *testing.T) {
	frozenNow := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	futureSource := Source{
		ID:          "future-source",
		Access:      SourceAccessTimeline,
		AvailableAt: frozenNow.Add(3 * time.Hour),
	}
	neverReleased := Source{
		ID:     "never-released",
		Access: SourceAccessTimeline,
	}
	pack := &Pack{Manifest: Manifest{
		FrozenNow: frozenNow,
		Sources:   []Source{futureSource, neverReleased},
		Episodes: []Episode{
			{
				ID:                  "episode",
				Kind:                EpisodeEvent,
				At:                  frozenNow.Add(2 * time.Hour),
				ReleaseSourceIDs:    []string{"missing-source", futureSource.ID},
				ExpectedArtifactIDs: []string{"missing-artifact"},
			},
			{
				ID:               "episode",
				Kind:             EpisodeHeartbeat,
				At:               frozenNow.Add(time.Hour),
				ReleaseSourceIDs: []string{futureSource.ID},
			},
		},
	}}
	v := &validator{
		pack: pack,
		sources: map[string]Source{
			futureSource.ID:  futureSource,
			neverReleased.ID: neverReleased,
		},
		artifacts: make(map[string]Artifact),
		released:  make(map[string]string),
	}

	v.validateEpisodes()

	want := []string{
		`episode "episode" releases unknown source "missing-source"`,
		`episode "episode" exposes future source "future-source" at 2026-07-01T11:00:00Z before availableAt 2026-07-01T12:00:00Z`,
		`episode "episode" expects unknown artifact "missing-artifact"`,
		`duplicate episode id "episode"`,
		`episode "episode" is out of chronological order`,
		`executable episode "episode" requires input`,
		`source "future-source" is released more than once by episodes "episode" and "episode"`,
		`episode "episode" exposes future source "future-source" at 2026-07-01T10:00:00Z before availableAt 2026-07-01T12:00:00Z`,
		`timeline source "never-released" is never released`,
	}
	if !slices.Equal(v.problems, want) {
		t.Fatalf("validateEpisodes problems:\n got: %s\nwant: %s", strings.Join(v.problems, "\n      "), strings.Join(want, "\n      "))
	}
}

func TestValidateEpisodesReturnsProblemsInPublicOrder(t *testing.T) {
	dir, manifest := writeValidCase(t)
	duplicate := manifest.Episodes[0]
	duplicate.At = manifest.FrozenNow.Add(-time.Minute)
	duplicate.Input = nil
	duplicate.ExpectedArtifactIDs = []string{"missing-artifact"}
	manifest.Episodes = append(manifest.Episodes, duplicate)
	writeManifest(t, dir, &manifest)

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir: expected validation error")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("LoadDir error = %T, want *ValidationError", err)
	}
	want := []string{
		`duplicate episode id "episode-1"`,
		`episode "episode-1" at 2026-07-01T08:59:00Z is before frozenNow 2026-07-01T09:00:00Z`,
		`episode "episode-1" expects unknown artifact "missing-artifact"`,
		`episode "episode-1" exposes future source "mail-new" at 2026-07-01T08:59:00Z before availableAt 2026-07-01T10:00:00Z`,
		`episode "episode-1" is out of chronological order`,
		`executable episode "episode-1" requires input`,
		`source "mail-new" is released more than once by episodes "episode-1" and "episode-1"`,
	}
	if !slices.Equal(validationErr.Problems, want) {
		t.Fatalf("ValidationError.Problems:\n got: %s\nwant: %s", strings.Join(validationErr.Problems, "\n      "), strings.Join(want, "\n      "))
	}
}
