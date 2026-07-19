package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Capability is a query/retrieval property exposed by one source connector.
// The planner reasons over these typed values rather than source-name prose.
type Capability string

const (
	CapabilityLexical     Capability = "lexical"
	CapabilitySemantic    Capability = "semantic"
	CapabilityStructured  Capability = "structured"
	CapabilityGraph       Capability = "graph"
	CapabilityCode        Capability = "code"
	CapabilityRerank      Capability = "rerank"
	CapabilityLateContext Capability = "late-context"
)

// SyncContract is the common ingestion lifecycle every knowledge connector
// describes. A connector may be event-driven or snapshot-based, but it must
// make stable identity, replay position, change/deletion detection, freshness,
// and authorization boundaries explicit.
type SyncContract struct {
	StableID              string // how one source object is identified across replays
	Cursor                string // watermark/replay contract (event ID, timestamp, snapshot revision, ...)
	ChangeDetection       string // content hash, revision, mtime+size+hash, ...
	DeletionDetection     string // tombstone, snapshot diff, explicit delete event, ...
	FreshnessTargetMillis int64
	AuthorizationBoundary string // ACL/workspace/source permission boundary
}

// Validate catches connector definitions that would silently duplicate,
// retain deleted content, or lose replay/freshness semantics.
func (c SyncContract) Validate() error {
	missing := make([]string, 0, 5)
	if strings.TrimSpace(c.StableID) == "" {
		missing = append(missing, "stable_id")
	}
	if strings.TrimSpace(c.Cursor) == "" {
		missing = append(missing, "cursor")
	}
	if strings.TrimSpace(c.ChangeDetection) == "" {
		missing = append(missing, "change_detection")
	}
	if strings.TrimSpace(c.DeletionDetection) == "" {
		missing = append(missing, "deletion_detection")
	}
	if strings.TrimSpace(c.AuthorizationBoundary) == "" {
		missing = append(missing, "authorization_boundary")
	}
	if len(missing) > 0 {
		return fmt.Errorf("knowledge sync contract missing %s", strings.Join(missing, ", "))
	}
	if c.FreshnessTargetMillis < 0 {
		return fmt.Errorf("knowledge sync contract freshness target must be non-negative")
	}
	return nil
}

// SyncEnvelope is the normalized mutation carried by a connector. Deleted
// envelopes intentionally keep StableID/Revision so tombstones participate in
// replay and idempotency instead of becoming an out-of-band cleanup path.
type SyncEnvelope struct {
	StableID    string
	Revision    string
	ContentHash string
	Deleted     bool
	ObservedAt  int64
	ACL         []string
}

func (e SyncEnvelope) Validate() error {
	if strings.TrimSpace(e.StableID) == "" {
		return fmt.Errorf("knowledge sync envelope requires stable id")
	}
	if strings.TrimSpace(e.Revision) == "" && strings.TrimSpace(e.ContentHash) == "" {
		return fmt.Errorf("knowledge sync envelope %q requires revision or content hash", e.StableID)
	}
	if e.ObservedAt < 0 {
		return fmt.Errorf("knowledge sync envelope %q has negative observed time", e.StableID)
	}
	return nil
}

// SameRevision is the replay/dedup primitive: identity and a stable revision
// (or content hash fallback) must agree. Deletion state is part of the revision
// so an explicit tombstone is never deduped against the live record it removes.
func (e SyncEnvelope) SameRevision(other SyncEnvelope) bool {
	if e.StableID == "" || e.StableID != other.StableID || e.Deleted != other.Deleted {
		return false
	}
	if e.Revision != "" && other.Revision != "" {
		return e.Revision == other.Revision
	}
	return e.ContentHash != "" && e.ContentHash == other.ContentHash
}

// SourceDescriptor is the compact catalog entry used by the planner and
// emitted in its plan. Cost is a relative class (1 cheap/local, 3 slower
// model/network path), not a latency promise.
type SourceDescriptor struct {
	Layer        Layer
	Name         string
	Description  string
	Capabilities []Capability
	Cost         int
	Sync         SyncContract
}

// SourceDescriber is optional for third-party adapters. Built-in adapters
// implement it; unknown adapters receive a safe fallback descriptor.
type SourceDescriber interface {
	Descriptor() SourceDescriptor
}

// Locator preserves exact evidence coordinates separately from the expanded
// context window that synthesis sees.
type Locator struct {
	StartLine        int
	EndLine          int
	ContextStartLine int
	ContextEndLine   int
}

type FreshnessState string

const (
	FreshnessUnknown FreshnessState = "unknown"
	FreshnessCurrent FreshnessState = "current"
	FreshnessStale   FreshnessState = "stale"
)

type Freshness struct {
	State        FreshnessState
	AgeMillis    int64
	TargetMillis int64
}

// Provenance is the common evidence schema populated for every adapter hit.
type Provenance struct {
	Source      string
	StableID    string
	ContentHash string
	RetrievedAt int64
	ObservedAt  int64
	Locator     Locator
	Hierarchy   []string
	Freshness   Freshness
}

func fallbackDescriptor(layer Layer) SourceDescriptor {
	return SourceDescriptor{
		Layer: layer,
		Name:  string(layer),
		Cost:  1,
		Sync: SyncContract{
			StableID: "adapter-defined id", Cursor: "adapter-defined replay position",
			ChangeDetection: "adapter-defined revision", DeletionDetection: "adapter-defined delete",
			AuthorizationBoundary: "adapter boundary",
		},
	}
}

func descriptorOf(adapter Adapter) SourceDescriptor {
	descriptor := fallbackDescriptor(adapter.Layer())
	if described, ok := adapter.(SourceDescriber); ok {
		descriptor = described.Descriptor()
	}
	if descriptor.Layer == "" {
		descriptor.Layer = adapter.Layer()
	}
	if descriptor.Name == "" {
		descriptor.Name = string(descriptor.Layer)
	}
	return descriptor
}

func normalizeEvidence(result Result, descriptor SourceDescriptor, retrievedAt int64) Result {
	if result.Ref.Layer == "" {
		result.Ref.Layer = descriptor.Layer
	}
	result.Provenance.Source = descriptor.Name
	if result.Provenance.StableID == "" {
		result.Provenance.StableID = result.Ref.ID
	}
	result.Provenance.RetrievedAt = retrievedAt
	if result.Provenance.ObservedAt == 0 {
		result.Provenance.ObservedAt = result.Time
	}
	if result.Provenance.ContentHash == "" {
		sum := sha256.Sum256([]byte(result.Ref.String() + "\x00" + result.Snippet + "\x00" + result.Context))
		result.Provenance.ContentHash = hex.EncodeToString(sum[:8])
	}
	target := descriptor.Sync.FreshnessTargetMillis
	result.Provenance.Freshness = freshnessAt(result.Provenance.ObservedAt, retrievedAt, target)
	return result
}

func freshnessAt(observedAt, now, target int64) Freshness {
	freshness := Freshness{State: FreshnessUnknown, TargetMillis: target}
	if observedAt <= 0 || now <= 0 {
		return freshness
	}
	freshness.AgeMillis = max(int64(0), now-observedAt)
	freshness.State = FreshnessCurrent
	if target > 0 && freshness.AgeMillis > target {
		freshness.State = FreshnessStale
	}
	return freshness
}

func pathHierarchy(path string) []string {
	path = strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for i := range parts {
		out = append(out, strings.Join(parts[:i+1], "/"))
	}
	return out
}

func millis(duration time.Duration) int64 { return duration.Milliseconds() }
