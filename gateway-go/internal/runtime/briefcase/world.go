package briefcase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

var (
	ErrSourceNotVisible = errors.New("briefcase source is not visible")
	ErrSourceNotDue     = errors.New("briefcase source is not available yet")
)

// Record is an agent-visible case source. Content is copied at every API
// boundary so callers cannot mutate the world's immutable evidence.
type Record struct {
	Source  casepack.Source `json:"source"`
	Content []byte          `json:"content"`
}

type RecordPreview struct {
	Record
	ContentBytes int  `json:"contentBytes"`
	Truncated    bool `json:"truncated"`
	OffsetBytes  int  `json:"offsetBytes"`
	NextOffset   int  `json:"nextOffset,omitempty"`
}

// World is the time-bounded source view for one run. It loads only snapshot
// sources at construction and opens timeline sources atomically when an episode
// releases them. Sealed sources are never read into this process component.
type World struct {
	mu       sync.RWMutex
	pack     *casepack.Pack
	clock    Clock
	sources  map[string]casepack.Source
	visible  map[string]Record
	released map[string]struct{}
	withheld map[string]struct{}
}

func NewWorld(pack *casepack.Pack, clock Clock) (*World, error) {
	return NewWorldWithOptions(pack, clock, WorldOptions{IncludeMemory: true})
}

type WorldOptions struct {
	// IncludeMemory controls only sources explicitly marked Source.Memory.
	// Primary sources remain identical in both benchmark arms.
	IncludeMemory bool
}

// ReleaseOutcome distinguishes sources actually exposed to the agent from
// memory sources intentionally withheld by the raw-primary arm.
type ReleaseOutcome struct {
	Released []string `json:"releasedSourceIds,omitempty"`
	Withheld []string `json:"withheldSourceIds,omitempty"`
}

func NewWorldWithOptions(pack *casepack.Pack, clock Clock, opts WorldOptions) (*World, error) {
	if pack == nil || clock == nil {
		return nil, errors.New("briefcase: pack and clock are required")
	}
	if err := casepack.Validate(pack); err != nil {
		return nil, fmt.Errorf("briefcase: validate world pack: %w", err)
	}
	w := &World{
		pack:     pack,
		clock:    clock,
		sources:  make(map[string]casepack.Source, len(pack.Manifest.Sources)),
		visible:  make(map[string]Record),
		released: make(map[string]struct{}),
		withheld: make(map[string]struct{}),
	}
	for _, source := range pack.Manifest.Sources {
		w.sources[source.ID] = source
		if source.Memory && !opts.IncludeMemory {
			w.withheld[source.ID] = struct{}{}
			continue
		}
		if source.Access != casepack.SourceAccessSnapshot {
			continue
		}
		content, err := pack.ReadFile(source.Path)
		if err != nil {
			return nil, fmt.Errorf("briefcase: load snapshot source %q: %w", source.ID, err)
		}
		w.visible[source.ID] = Record{Source: source, Content: bytes.Clone(content)}
	}
	return w, nil
}

// Release atomically exposes timeline sources. A failed release leaves the
// visible set unchanged, which lets the timeline retry the same episode.
func (w *World) Release(sourceIDs []string) error {
	_, err := w.ReleaseWithOutcome(sourceIDs)
	return err
}

// ReleaseWithOutcome atomically validates a release and reports its effective
// arm-specific visibility. Withheld memory still obeys access, timing, and
// exactly-once release rules; the raw arm changes visibility, not history.
func (w *World) ReleaseWithOutcome(sourceIDs []string) (ReleaseOutcome, error) {
	return w.ReleaseWithOutcomeContext(context.Background(), sourceIDs)
}

func (w *World) ReleaseWithOutcomeContext(ctx context.Context, sourceIDs []string) (ReleaseOutcome, error) {
	if w == nil {
		return ReleaseOutcome{}, errors.New("briefcase: nil world")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	now := canonicalTime(w.clock.Now())
	pending := make([]Record, 0, len(sourceIDs))
	outcome := ReleaseOutcome{
		Released: make([]string, 0, len(sourceIDs)),
		Withheld: make([]string, 0, len(sourceIDs)),
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if err := ctx.Err(); err != nil {
			return ReleaseOutcome{}, err
		}
		if _, duplicate := seen[id]; duplicate {
			return ReleaseOutcome{}, fmt.Errorf("briefcase: duplicate source release %q", id)
		}
		seen[id] = struct{}{}
		source, ok := w.sources[id]
		if !ok {
			return ReleaseOutcome{}, fmt.Errorf("%w: unknown source %q", ErrSourceNotVisible, id)
		}
		if source.Access != casepack.SourceAccessTimeline {
			return ReleaseOutcome{}, fmt.Errorf("%w: source %q has access %q", ErrSourceNotVisible, id, source.Access)
		}
		if _, already := w.released[id]; already {
			return ReleaseOutcome{}, fmt.Errorf("%w: source %q was already released", ErrSourceNotVisible, id)
		}
		if canonicalTime(source.AvailableAt).After(now) {
			return ReleaseOutcome{}, fmt.Errorf("%w: source %q available at %s, now %s", ErrSourceNotDue, id, source.AvailableAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		}
		if _, withheld := w.withheld[id]; withheld {
			outcome.Withheld = append(outcome.Withheld, id)
			continue
		}
		content, err := w.pack.ReadFile(source.Path)
		if err != nil {
			return ReleaseOutcome{}, fmt.Errorf("briefcase: load timeline source %q: %w", id, err)
		}
		if err := ctx.Err(); err != nil {
			return ReleaseOutcome{}, err
		}
		pending = append(pending, Record{Source: source, Content: bytes.Clone(content)})
		outcome.Released = append(outcome.Released, id)
	}
	for _, id := range sourceIDs {
		w.released[id] = struct{}{}
	}
	for _, record := range pending {
		w.visible[record.Source.ID] = record
	}
	return outcome, nil
}

func (w *World) Get(sourceID string) (Record, error) {
	if w == nil {
		return Record{}, ErrSourceNotVisible
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	record, ok := w.visible[sourceID]
	if !ok {
		return Record{}, fmt.Errorf("%w: %q", ErrSourceNotVisible, sourceID)
	}
	return cloneRecord(record), nil
}

func (w *World) GetPreviewContext(ctx context.Context, sourceID string, maxContentBytes int) (RecordPreview, error) {
	return w.GetPreviewRangeContext(ctx, sourceID, 0, maxContentBytes)
}

func (w *World) GetPreviewRangeContext(ctx context.Context, sourceID string, offsetBytes, maxContentBytes int) (RecordPreview, error) {
	if w == nil {
		return RecordPreview{}, ErrSourceNotVisible
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.RLock()
	record, ok := w.visible[sourceID]
	if !ok {
		w.mu.RUnlock()
		return RecordPreview{}, fmt.Errorf("%w: %q", ErrSourceNotVisible, sourceID)
	}
	preview, err := previewRecordContext(ctx, record, offsetBytes, maxContentBytes)
	w.mu.RUnlock()
	return preview, err
}

func (w *World) QueryPreviewsContext(ctx context.Context, kinds []casepack.SourceKind, query string, recordOffset, maxRecords, maxContentBytes, maxTotalContentBytes int) ([]RecordPreview, int, error) {
	if w == nil {
		return nil, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	allowed := make(map[casepack.SourceKind]struct{}, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	w.mu.RLock()
	defer w.mu.RUnlock()
	matches, matchOffsets, err := w.collectPreviewMatches(ctx, allowed, needle)
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(matches, func(i, j int) bool {
		left, right := matches[i].Source.AvailableAt, matches[j].Source.AvailableAt
		if !left.Equal(right) {
			return left.Before(right)
		}
		return matches[i].Source.ID < matches[j].Source.ID
	})
	total := len(matches)
	if recordOffset < 0 || recordOffset > total {
		return nil, total, fmt.Errorf("briefcase: record offset %d is outside 0..%d", recordOffset, total)
	}
	matches = matches[recordOffset:]
	if maxRecords > 0 && len(matches) > maxRecords {
		matches = matches[:maxRecords]
	}
	previews, err := buildRecordPreviews(ctx, matches, matchOffsets, needle, maxContentBytes, maxTotalContentBytes)
	if err != nil {
		return nil, 0, err
	}
	return previews, total, ctx.Err()
}

// collectPreviewMatches gathers visible records that pass the kind filter and,
// when needle is non-empty, contain it — recording each match's byte offset by
// source ID. Caller must hold w.mu.
func (w *World) collectPreviewMatches(ctx context.Context, allowed map[casepack.SourceKind]struct{}, needle string) ([]Record, map[string]int, error) {
	matches := make([]Record, 0, len(w.visible))
	matchOffsets := make(map[string]int, len(w.visible))
	for _, record := range w.visible {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(allowed) > 0 {
			if _, ok := allowed[record.Source.Kind]; !ok {
				continue
			}
		}
		if needle != "" {
			matched, offset, err := recordMatchOffsetContext(ctx, record, needle)
			if err != nil {
				return nil, nil, err
			}
			if !matched {
				continue
			}
			matchOffsets[record.Source.ID] = offset
		}
		matches = append(matches, record)
	}
	return matches, matchOffsets, nil
}

// buildRecordPreviews renders one preview per match, centering each window on
// the record's needle offset and charging every rendered byte against the
// shared maxTotalContentBytes budget (negative budget = unlimited).
func buildRecordPreviews(ctx context.Context, matches []Record, matchOffsets map[string]int, needle string, maxContentBytes, maxTotalContentBytes int) ([]RecordPreview, error) {
	previews := make([]RecordPreview, 0, len(matches))
	remaining := maxTotalContentBytes
	for _, record := range matches {
		limit := maxContentBytes
		if remaining >= 0 && (limit <= 0 || remaining < limit) {
			limit = remaining
		}
		offset := 0
		if needle != "" && limit > 0 {
			offset = matchOffsets[record.Source.ID] - limit/2
			if offset < 0 {
				offset = 0
			}
			if offset > len(record.Content) {
				offset = len(record.Content)
			}
		}
		preview, err := previewRecordContext(ctx, record, offset, limit)
		if err != nil {
			return nil, err
		}
		previews = append(previews, preview)
		if remaining >= 0 {
			remaining -= len(preview.Content)
		}
	}
	return previews, nil
}

func previewRecordContext(ctx context.Context, record Record, offsetBytes, maxContentBytes int) (RecordPreview, error) {
	originalBytes := len(record.Content)
	if offsetBytes < 0 || offsetBytes > originalBytes {
		return RecordPreview{}, fmt.Errorf("briefcase: record offset %d is outside 0..%d", offsetBytes, originalBytes)
	}
	end := originalBytes
	if maxContentBytes >= 0 && end-offsetBytes > maxContentBytes {
		end = offsetBytes + maxContentBytes
	}
	if utf8.Valid(record.Content) {
		for offsetBytes > 0 && offsetBytes < originalBytes && !utf8.RuneStart(record.Content[offsetBytes]) {
			offsetBytes--
		}
		for end < originalBytes && !utf8.RuneStart(record.Content[end]) {
			end++
		}
	}
	content := make([]byte, end-offsetBytes)
	for start := offsetBytes; start < end; start += 64 * 1024 {
		if err := ctx.Err(); err != nil {
			return RecordPreview{}, err
		}
		end := start + 64*1024
		if end > offsetBytes+len(content) {
			end = offsetBytes + len(content)
		}
		copy(content[start-offsetBytes:end-offsetBytes], record.Content[start:end])
	}
	record.Content = content
	record.Source.ProjectRefs = append([]string(nil), record.Source.ProjectRefs...)
	record.Source.Supersedes = append([]string(nil), record.Source.Supersedes...)
	nextOffset := 0
	if end < originalBytes {
		nextOffset = end
	}
	return RecordPreview{
		Record: record, ContentBytes: originalBytes, Truncated: offsetBytes > 0 || end < originalBytes,
		OffsetBytes: offsetBytes, NextOffset: nextOffset,
	}, ctx.Err()
}

// Query returns visible records in deterministic available-time/ID order.
// kinds=nil and query="" list the complete visible set.
func (w *World) Query(kinds []casepack.SourceKind, query string) []Record {
	records, _ := w.QueryContext(context.Background(), kinds, query)
	return records
}

func (w *World) QueryContext(ctx context.Context, kinds []casepack.SourceKind, query string) ([]Record, error) {
	if w == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	allowed := make(map[casepack.SourceKind]struct{}, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	needle := strings.ToLower(strings.TrimSpace(query))

	w.mu.RLock()
	out := make([]Record, 0, len(w.visible))
	for _, record := range w.visible {
		if err := ctx.Err(); err != nil {
			w.mu.RUnlock()
			return nil, err
		}
		if len(allowed) > 0 {
			if _, ok := allowed[record.Source.Kind]; !ok {
				continue
			}
		}
		if needle != "" {
			matches, err := recordMatchesContext(ctx, record, needle)
			if err != nil {
				w.mu.RUnlock()
				return nil, err
			}
			if !matches {
				continue
			}
		}
		clone, err := cloneRecordContext(ctx, record)
		if err != nil {
			w.mu.RUnlock()
			return nil, err
		}
		out = append(out, clone)
	}
	w.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].Source.AvailableAt, out[j].Source.AvailableAt
		if !left.Equal(right) {
			return left.Before(right)
		}
		return out[i].Source.ID < out[j].Source.ID
	})
	return out, ctx.Err()
}

func (w *World) VisibleSourceIDs() []string {
	ids, _ := w.VisibleSourceIDsContext(context.Background())
	return ids
}

func (w *World) VisibleSourceIDsContext(ctx context.Context) ([]string, error) {
	if w == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type visibleKey struct {
		id string
		at time.Time
	}
	w.mu.RLock()
	keys := make([]visibleKey, 0, len(w.visible))
	for _, record := range w.visible {
		if err := ctx.Err(); err != nil {
			w.mu.RUnlock()
			return nil, err
		}
		keys = append(keys, visibleKey{id: record.Source.ID, at: record.Source.AvailableAt})
	}
	w.mu.RUnlock()
	sort.Slice(keys, func(i, j int) bool {
		if !keys[i].at.Equal(keys[j].at) {
			return keys[i].at.Before(keys[j].at)
		}
		return keys[i].id < keys[j].id
	})
	ids := make([]string, len(keys))
	for i, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ids[i] = key.id
	}
	return ids, ctx.Err()
}

// Materialize writes the visible view into workspace/records using opaque,
// validated source IDs. Existing files must be byte-identical; this prevents a
// model from altering a record and having a later release silently restore it.
func (w *World) Materialize(root *RunRoot) error {
	return w.MaterializeContext(context.Background(), root)
}

func (w *World) MaterializeContext(ctx context.Context, root *RunRoot) error {
	if w == nil || root == nil {
		return errors.New("briefcase: world and run root are required")
	}
	paths, err := root.Paths()
	if err != nil {
		return err
	}
	records, err := w.QueryContext(ctx, nil, "")
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind := string(record.Source.Kind)
		id := record.Source.ID
		if !safeSegment(kind) || !safeSegment(id) {
			return fmt.Errorf("briefcase: unsafe record materialization path kind=%q id=%q", kind, id)
		}
		dir := filepath.Join(paths.Workspace, "records", kind)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("briefcase: create record directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("briefcase: secure record directory: %w", err)
		}
		path := filepath.Join(dir, id+".source")
		if _, readErr := os.Lstat(path); readErr == nil {
			equal, compareErr := fileEqualsBytesContext(ctx, path, record.Content)
			if compareErr != nil {
				return fmt.Errorf("briefcase: inspect materialized source %q: %w", id, compareErr)
			}
			if !equal {
				return fmt.Errorf("briefcase: materialized source %q was modified", id)
			}
			continue
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("briefcase: inspect materialized source %q: %w", id, readErr)
		}
		if err := atomicfile.WriteFileContext(ctx, path, record.Content, nil); err != nil {
			return fmt.Errorf("briefcase: materialize source %q: %w", id, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	outputDir := filepath.Join(paths.Workspace, "output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("briefcase: create output directory: %w", err)
	}
	return os.Chmod(outputDir, 0o700)
}

func fileEqualsBytesContext(ctx context.Context, path string, expected []byte) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return false, err
	}
	buffer := make([]byte, 64*1024)
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			if !bytes.Equal(buffer[:n], expected[offset:offset+n]) {
				return false, nil
			}
			offset += n
		}
		if errors.Is(readErr, io.EOF) {
			return true, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

// StateJSON is a stable minimal state projection suitable for deterministic
// state checks. Source contents are deliberately excluded.
func (w *World) StateJSON() json.RawMessage {
	state := struct {
		Now              time.Time `json:"now"`
		VisibleSourceIDs []string  `json:"visibleSourceIds"`
	}{Now: canonicalTime(w.clock.Now()), VisibleSourceIDs: w.VisibleSourceIDs()}
	data, _ := json.Marshal(state)
	return data
}

func recordMatchesContext(ctx context.Context, record Record, needle string) (bool, error) {
	matched, _, err := recordMatchOffsetContext(ctx, record, needle)
	return matched, err
}

func recordMatchOffsetContext(ctx context.Context, record Record, needle string) (bool, int, error) {
	fields := []string{
		record.Source.ID,
		string(record.Source.Kind),
		record.Source.SourceRef,
		strings.Join(record.Source.ProjectRefs, " "),
	}
	if strings.Contains(strings.ToLower(strings.Join(fields, "\n")), needle) {
		return true, 0, nil
	}
	const chunkBytes = 64 * 1024
	overlap := len(needle) * 4
	if overlap < 64 {
		overlap = 64
	}
	if overlap > chunkBytes/2 {
		overlap = chunkBytes / 2
	}
	for start := 0; start < len(record.Content); {
		if err := ctx.Err(); err != nil {
			return false, 0, err
		}
		end := start + chunkBytes
		if end > len(record.Content) {
			end = len(record.Content)
		}
		chunk := string(record.Content[start:end])
		lowerChunk := strings.ToLower(chunk)
		if index := strings.Index(lowerChunk, needle); index >= 0 {
			runeIndex := utf8.RuneCountInString(lowerChunk[:index])
			originalOffset := 0
			seenRunes := 0
			for byteIndex := range chunk {
				if seenRunes == runeIndex {
					originalOffset = byteIndex
					break
				}
				seenRunes++
			}
			if runeIndex >= utf8.RuneCountInString(chunk) {
				originalOffset = len(chunk)
			}
			return true, start + originalOffset, nil
		}
		if end == len(record.Content) {
			break
		}
		start = end - overlap
	}
	return false, 0, ctx.Err()
}

func cloneRecord(record Record) Record {
	clone, _ := cloneRecordContext(context.Background(), record)
	return clone
}

func cloneRecordContext(ctx context.Context, record Record) (Record, error) {
	content := make([]byte, len(record.Content))
	for start := 0; start < len(record.Content); start += 64 * 1024 {
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		end := start + 64*1024
		if end > len(record.Content) {
			end = len(record.Content)
		}
		copy(content[start:end], record.Content[start:end])
	}
	record.Content = content
	record.Source.ProjectRefs = append([]string(nil), record.Source.ProjectRefs...)
	record.Source.Supersedes = append([]string(nil), record.Source.Supersedes...)
	return record, ctx.Err()
}

func safeSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.ContainsAny(value, `/\\`) && !strings.ContainsRune(value, '\x00')
}

// Clock returns the world clock.
func (w *World) Clock() Clock {
	if w == nil {
		return nil
	}
	return w.clock
}
