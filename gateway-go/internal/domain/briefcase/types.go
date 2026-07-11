// Package briefcase defines the portable, immutable case format used by the
// Deneb-Briefcase evaluation harness. It deliberately contains no runtime or
// model dependencies: a case can be authenticated and rejected before any
// agent process is started.
package briefcase

import "time"

const (
	// SchemaVersionV1 is the only schema accepted by this implementation.
	SchemaVersionV1 = "deneb.briefcase/v1"
	ManifestFile    = "manifest.json"
	// MaxTurnsV1 is a format-level guardrail, not merely a runner default.
	// A signed case cannot ask the runtime for an unbounded agent loop.
	MaxTurnsV1     = 500
	MaxFollowUpsV1 = 2
	MaxSourcesV1   = 512
	MaxEpisodesV1  = 500
	MaxArtifactsV1 = 128
	MaxToolRulesV1 = 32
	MaxToolCallsV1 = 10_000
	MaxTokensV1    = 250_000
	// MaxEpisodeInputBytesV1 bounds the exact user message injected for one
	// executable timeline episode.
	MaxEpisodeInputBytesV1 = 64 << 10
	// MaxArtifactBytesV1 is the hard hashing/snapshot ceiling when an artifact
	// omits a tighter signed maxBytes value.
	MaxArtifactBytesV1 = 64 << 20
	// MaxRecallSourceBytesV1 bounds text projected into the in-memory Wiki
	// recall index. Binary-only record sources retain the general asset cap.
	MaxRecallSourceBytesV1 = 4 << 20
	// MaxTimeoutSecondsV1 prevents a signed Portable case from becoming an
	// effectively unbounded local or remote workload.
	MaxTimeoutSecondsV1 = int64(24 * 60 * 60)
)

// Manifest is the complete, content-addressed contract for one briefcase.
// ManifestDigest authenticates the canonical form of the manifest with that
// field blank; each referenced input is authenticated independently by SHA256.
type Manifest struct {
	SchemaVersion  string      `json:"schemaVersion"`
	ManifestDigest string      `json:"manifestDigest"`
	CaseID         string      `json:"caseId"`
	FamilyID       string      `json:"familyId"`
	Split          Split       `json:"split"`
	PrivacyMode    PrivacyMode `json:"privacyMode"`
	// Seed is required and positive in v1. Zero is reserved to mean unset so
	// an omitted JSON field cannot silently select a valid experiment seed.
	Seed          int64         `json:"seed"`
	CutoffAt      time.Time     `json:"cutoffAt"`
	FrozenNow     time.Time     `json:"frozenNow"`
	Timezone      string        `json:"timezone"`
	Locale        string        `json:"locale"`
	Sources       []Source      `json:"sources"`
	Episodes      []Episode     `json:"episodes"`
	Artifacts     []Artifact    `json:"artifacts"`
	RunPolicy     RunPolicy     `json:"runPolicy"`
	ToolPolicy    ToolPolicy    `json:"toolPolicy"`
	NetworkPolicy NetworkPolicy `json:"networkPolicy"`
}

// RunPolicy is the signed execution budget. Runtime layers may choose tighter
// limits, but must never exceed these case-authorized maxima.
type RunPolicy struct {
	MaxTurns              int   `json:"maxTurns"`
	TimeoutSeconds        int64 `json:"timeoutSeconds"`
	MaxTokens             int64 `json:"maxTokens"`
	MaxFollowUps          int   `json:"maxFollowUps,omitempty"`
	PerTurnTimeoutSeconds int64 `json:"perTurnTimeoutSeconds,omitempty"`
}

type Split string

const (
	SplitDev         Split = "dev"
	SplitCalibration Split = "calibration"
	SplitHoldout     Split = "holdout"
)

type PrivacyMode string

const (
	PrivacyPortable PrivacyMode = "portable"
	PrivacyVault    PrivacyMode = "vault"
)

// SourceAccess controls when a source may enter the agent-visible world.
//
//   - snapshot: available in the initial read-only snapshot
//   - timeline: held by the harness and released by an episode
//   - sealed: grader-only evidence; never released to the agent
type SourceAccess string

const (
	SourceAccessSnapshot SourceAccess = "snapshot"
	SourceAccessTimeline SourceAccess = "timeline"
	SourceAccessSealed   SourceAccess = "sealed"
)

type SourceKind string

const (
	SourceMail       SourceKind = "mail"
	SourceCalendar   SourceKind = "calendar"
	SourceWiki       SourceKind = "wiki"
	SourceDiary      SourceKind = "diary"
	SourceCapture    SourceKind = "capture"
	SourceFile       SourceKind = "file"
	SourceNotebook   SourceKind = "notebook"
	SourceWorkfeed   SourceKind = "workfeed"
	SourceTranscript SourceKind = "transcript"
	SourceDevice     SourceKind = "device"
)

type SourceOrigin string

const (
	SourceOriginExternal       SourceOrigin = "external"
	SourceOriginHuman          SourceOrigin = "human"
	SourceOriginDenebGenerated SourceOrigin = "deneb-generated"
	SourceOriginSynthetic      SourceOrigin = "synthetic"
)

// Source describes one immutable evidence object. EventAt is when the fact or
// event occurred; AvailableAt is when Deneb could first have known it;
// CapturedAt is provenance and must never be used as a visibility shortcut.
type Source struct {
	ID          string       `json:"id"`
	Kind        SourceKind   `json:"kind"`
	Origin      SourceOrigin `json:"origin"`
	Access      SourceAccess `json:"access"`
	Path        string       `json:"path"`
	SHA256      string       `json:"sha256"`
	EventAt     time.Time    `json:"eventAt"`
	AvailableAt time.Time    `json:"availableAt"`
	CapturedAt  time.Time    `json:"capturedAt"`
	ProjectRefs []string     `json:"projectRefs,omitempty"`
	SourceRef   string       `json:"sourceRef,omitempty"`
	Supersedes  []string     `json:"supersedes,omitempty"`
	Sensitivity string       `json:"sensitivity,omitempty"`
	// Memory marks a derived durable-memory source. The raw-primary benchmark
	// arm withholds these records while memory-assisted exposes them; provenance
	// and access timing remain identical in both arms.
	Memory bool `json:"memory,omitempty"`
}

type EpisodeKind string

const (
	EpisodeUserTurn  EpisodeKind = "user-turn"
	EpisodeEvent     EpisodeKind = "event"
	EpisodeHeartbeat EpisodeKind = "heartbeat"
)

// FileRef binds a runtime-controlled file to its content. Episode input files
// live under timeline/ and are injected by the harness at At; they are never
// part of the agent's initial mounted snapshot.
type FileRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Episode advances the case clock and may atomically release timeline sources.
// ExpectedArtifactIDs names outputs that should exist after this episode.
type Episode struct {
	ID                  string      `json:"id"`
	Kind                EpisodeKind `json:"kind"`
	At                  time.Time   `json:"at"`
	Input               *FileRef    `json:"input,omitempty"`
	ReleaseSourceIDs    []string    `json:"releaseSourceIds,omitempty"`
	ExpectedArtifactIDs []string    `json:"expectedArtifactIds,omitempty"`
}

// Artifact is an agent-produced deliverable contract. Paths are always under
// output/ and therefore do not exist in an immutable input casepack.
type Artifact struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	MIME     string `json:"mime"`
	Required bool   `json:"required"`
	MaxBytes int64  `json:"maxBytes,omitempty"`
}

type ToolDecision string

const (
	ToolDeny     ToolDecision = "deny"
	ToolAllow    ToolDecision = "allow"
	ToolApproval ToolDecision = "approval"
)

type ToolPolicy struct {
	Default  ToolDecision `json:"default"`
	MaxCalls int          `json:"maxCalls"`
	Rules    []ToolRule   `json:"rules,omitempty"`
}

type ToolRule struct {
	Name     string       `json:"name"`
	Decision ToolDecision `json:"decision"`
	MaxCalls int          `json:"maxCalls,omitempty"`
}

type NetworkMode string

const (
	NetworkDeny NetworkMode = "deny"
)

type NetworkPolicy struct {
	Mode         NetworkMode `json:"mode"`
	AllowedHosts []string    `json:"allowedHosts,omitempty"`
}

// Pack is a validated directory-backed briefcase. Callers should use ReadFile
// rather than joining paths themselves so the same traversal and symlink
// checks remain in force after loading.
type Pack struct {
	Root     string
	Manifest Manifest
	Digest   string
	files    map[string]struct{}
	hashes   map[string]string
}
