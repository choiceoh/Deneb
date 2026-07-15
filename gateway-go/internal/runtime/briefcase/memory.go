package briefcase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// denebMemoryMirror projects currently visible wiki/diary records plus explicit
// memory-role records into Deneb's real Wiki store. Both arms receive the same
// production wiki tool schema; the raw World simply never exposes memory-role
// records, and only the assisted arm connects this store to automatic recall.
type denebMemoryMirror struct {
	store  *wiki.Store
	world  *World
	synced map[string]string
}

func newDenebMemoryMirror(paths RunPaths, world *World) (*denebMemoryMirror, error) {
	if world == nil {
		return nil, fmt.Errorf("briefcase: memory mirror requires a world")
	}
	base := filepath.Join(paths.State, "briefcase-memory")
	store, err := wiki.NewStoreWithSearchOptions(
		filepath.Join(base, "wiki"), filepath.Join(base, "diary"),
		wiki.SearchOptions{Now: world.clock.Now, FieldBoost: 2.5, BM25RarityFloor: 0.55},
	)
	if err != nil {
		return nil, fmt.Errorf("briefcase: create isolated Deneb memory store: %w", err)
	}
	return &denebMemoryMirror{store: store, world: world, synced: make(map[string]string)}, nil
}

// Sync makes newly visible memory records recallable. Rewrites occur only when
// either immutable content or its visible supersession marker changes.
func (m *denebMemoryMirror) Sync() error {
	return m.SyncContext(context.Background())
}

func (m *denebMemoryMirror) SyncContext(ctx context.Context) error {
	if m == nil || m.store == nil || m.world == nil {
		return nil
	}
	records, err := m.world.QueryContext(ctx, nil, "")
	if err != nil {
		return err
	}
	visible := make(map[string]Record, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		visible[record.Source.ID] = record
	}

	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !shouldMirrorToWiki(record.Source) {
			continue
		}
		supersededBy := visibleSuperseder(record.Source.ID, records)
		page := memoryPage(record, supersededBy)
		rendered := page.Render()
		digest := sha256.Sum256(rendered)
		fingerprint := hex.EncodeToString(digest[:])
		if m.synced[record.Source.ID] == fingerprint {
			continue
		}
		if err := m.store.WritePage(memoryPagePath(record.Source.ID), page); err != nil {
			return fmt.Errorf("briefcase: index memory source %q: %w", record.Source.ID, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		m.synced[record.Source.ID] = fingerprint
	}
	return nil
}

func shouldMirrorToWiki(source casepack.Source) bool {
	return source.Memory || source.Kind == casepack.SourceWiki || source.Kind == casepack.SourceDiary
}

func (m *denebMemoryMirror) Close() error {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.Close()
}

func memoryPage(record Record, supersededBy string) *wiki.Page {
	source := record.Source
	available := source.AvailableAt.UTC()
	date := available.Format("2006-01-02")
	projectRefs := append([]string(nil), source.ProjectRefs...)
	sort.Strings(projectRefs)
	tags := []string{"briefcase-record", "source-" + string(source.Kind)}
	role := "record"
	section := "기록 내용"
	if source.Memory {
		tags = append(tags, "briefcase-memory")
		role = "durable-memory"
		section = "기억 내용"
	}
	for _, ref := range projectRefs {
		tags = append(tags, "project-"+ref)
	}
	page := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID:           source.ID,
			Title:        source.ID,
			Summary:      fmt.Sprintf("Briefcase %s %s available %s", role, source.Kind, available.Format(time.RFC3339)),
			Category:     "기타",
			Tags:         tags,
			Resource:     source.SourceRef,
			Created:      date,
			Updated:      date,
			Importance:   0.49, // searchable, but below the normal tier-1 auto-injection threshold
			Type:         "source",
			Confidence:   "high",
			SupersededBy: supersededBy,
		},
	}
	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", source.ID)
	fmt.Fprintf(&body, "- source kind: %s\n", source.Kind)
	fmt.Fprintf(&body, "- event at: %s\n", source.EventAt.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&body, "- available at: %s\n", available.Format(time.RFC3339Nano))
	if source.SourceRef != "" {
		fmt.Fprintf(&body, "- source ref: %s\n", source.SourceRef)
	}
	if len(projectRefs) > 0 {
		fmt.Fprintf(&body, "- project refs: %s\n", strings.Join(projectRefs, ", "))
	}
	if len(source.Supersedes) > 0 {
		fmt.Fprintf(&body, "- supersedes: %s\n", strings.Join(source.Supersedes, ", "))
	}
	fmt.Fprintf(&body, "\n## %s\n\n", section)
	body.Write(record.Content)
	page.Body = body.String()
	return page
}

func visibleSuperseder(sourceID string, records []Record) string {
	for _, candidate := range records {
		for _, prior := range candidate.Source.Supersedes {
			if prior == sourceID {
				return "briefcase:" + candidate.Source.ID
			}
		}
	}
	return ""
}

func memoryPagePath(sourceID string) string {
	digest := sha256.Sum256([]byte(sourceID))
	return filepath.ToSlash(filepath.Join("기타", "briefcase-"+hex.EncodeToString(digest[:12])+".md"))
}
