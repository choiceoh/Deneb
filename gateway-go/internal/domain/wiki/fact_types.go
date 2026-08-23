package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// FactRevision is the monotonic cursor of the durable fact mutation journal.
// Revision 0 is the empty baseline; every accepted assertion or tombstone gets
// exactly one larger revision.
type FactRevision uint64

// FactKind selects the authority policy used to resolve competing claims.
// The policies mirror the operator contract: direct user statements govern
// personal facts, dated primary documents govern commercial facts, and live
// observations govern system state. Generic claims deliberately keep peers in
// conflict instead of inventing a winner.
type FactKind string

const (
	FactKindGeneric     FactKind = "generic"
	FactKindPreference  FactKind = "preference"
	FactKindIdentity    FactKind = "identity"
	FactKindAmount      FactKind = "amount"
	FactKindDeadline    FactKind = "deadline"
	FactKindContract    FactKind = "contract"
	FactKindSystemState FactKind = "system_state"
)

// FactAuthority describes where a claim came from. It is intentionally not a
// single global trust score: authorityRank applies it through FactKind.
type FactAuthority string

const (
	FactAuthorityDirectUser   FactAuthority = "direct_user"
	FactAuthorityPrimaryDoc   FactAuthority = "primary_document"
	FactAuthorityRuntime      FactAuthority = "runtime_observation"
	FactAuthorityAgent        FactAuthority = "agent_confirmed"
	FactAuthorityInference    FactAuthority = "inference"
	FactAuthorityLegacyImport FactAuthority = "legacy_import"
)

// FactStatus is the lifecycle state of one immutable claim version.
type FactStatus string

const (
	FactStatusCurrent    FactStatus = "current"
	FactStatusConflicted FactStatus = "conflicted"
	FactStatusSuperseded FactStatus = "superseded"
	FactStatusTombstoned FactStatus = "tombstoned"
)

// FactInput is the caller-facing assertion contract. At and BasisAt are kept as
// time.Time so tests and importers can supply deterministic clocks without
// leaking formatting concerns into callers.
type FactInput struct {
	Subject   string
	Key       string
	Value     string
	Kind      FactKind
	Authority FactAuthority
	Sources   []string
	Actor     string
	Reason    string
	At        time.Time
	BasisAt   time.Time
}

// FactTombstoneInput retires every active claim for one identity without
// deleting its history. Authority is recorded for audit; an explicit forget is
// a lifecycle operation rather than another competing value.
type FactTombstoneInput struct {
	Subject   string
	Key       string
	Authority FactAuthority
	Sources   []string
	Actor     string
	Reason    string
	At        time.Time
}

// FactClaim is one immutable version retained by the active snapshot. The
// append-only journal retains the same version permanently even after it is
// superseded or tombstoned.
type FactClaim struct {
	ID           string        `json:"id"`
	Subject      string        `json:"subject"`
	Key          string        `json:"key"`
	Value        string        `json:"value"`
	Kind         FactKind      `json:"kind"`
	Authority    FactAuthority `json:"authority"`
	Status       FactStatus    `json:"status"`
	Sources      []string      `json:"sources,omitempty"`
	Actor        string        `json:"actor,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	RecordedAtMs int64         `json:"recordedAtMs"`
	BasisAtMs    int64         `json:"basisAtMs,omitempty"`
	Revision     FactRevision  `json:"revision"`
}

// FactMutation is the durable event applied to the current fact snapshot.
// StatusUpdates names older immutable claim IDs whose active disposition
// changed in the same atomic decision.
type FactMutation struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Revision      FactRevision          `json:"revision"`
	OperationID   string                `json:"operationId"`
	AtMs          int64                 `json:"atMs"`
	Op            string                `json:"op"` // assert | reaffirm | tombstone
	Identity      string                `json:"identity"`
	Claim         *FactClaim            `json:"claim,omitempty"`
	StatusUpdates map[string]FactStatus `json:"statusUpdates,omitempty"`
	Resolution    string                `json:"resolution"`
	Reason        string                `json:"reason,omitempty"`
}

// FactWriteResult reports the resolution selected for an assertion. A
// committed fact remains durable even if a compatibility projection fails;
// callers can surface ProjectionError without retrying the canonical write.
type FactWriteResult struct {
	Revision        FactRevision
	ClaimID         string
	Status          FactStatus
	Resolution      string
	Committed       bool
	ProjectionError string
}

// FactSnapshot is a safe copy of the fact plane at one revision.
type FactSnapshot struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Revision      FactRevision           `json:"revision"`
	Facts         map[string][]FactClaim `json:"facts"`
}

// FactLifecycleRule keeps correction and tombstone policy scoped to one
// canonical identity. Retired values must never be flattened across subjects:
// the same short value can be current for one project and stale for another.
type FactLifecycleRule struct {
	Subject       string
	Key           string
	Kind          FactKind
	CurrentValues []string
	StaleValues   []string
	Tombstoned    bool
}

// FactLifecycleEvidence is the shared identity context used to guard results
// from wiki search, federated knowledge backends, and chat recall sources.
// Ref is a stable page/file/source identifier; SubjectID and FactKey are
// optional typed hints when a backend has them.
type FactLifecycleEvidence struct {
	Query     string
	Ref       string
	SubjectID string
	FactKey   string
	Text      string
}

// FactRecallSnapshot is one atomic retrieval epoch. Active winners, typed
// lifecycle rules, and untyped legacy stale lines must be read under the same
// factMu lock; combining separate calls can otherwise expose old evidence
// beside a newer winner during a concurrent correction.
type FactRecallSnapshot struct {
	Revision       FactRevision
	Active         []FactClaim
	LifecycleRules []FactLifecycleRule
	// StaleValues contains only untyped lines extracted from legacy
	// superseded pages. Typed claim history lives in LifecycleRules and must
	// never be applied as a global string deny set.
	StaleValues []string
	// CorrectedKeys is retained for diagnostics/backward compatibility. Recall
	// enforcement uses LifecycleRules so current values stay subject-scoped.
	CorrectedKeys []string
}

const (
	factSchemaVersion    = 1
	factIdentityMaxRunes = 128
	factValueMaxRunes    = 2048
	factSourceMaxRunes   = 512
	factSourcesMax       = 16
	factAuditMaxRunes    = 512
)

var factKeyInvalidRE = regexp.MustCompile(`[^\pL\pN._:/-]+`)

func validateFactInputBounds(input FactInput) error {
	if err := validateFactRuneLimit("subject", input.Subject, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("key", input.Key, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("value", input.Value, factValueMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("actor", input.Actor, factAuditMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("reason", input.Reason, factAuditMaxRunes); err != nil {
		return err
	}
	return validateFactSourceBounds(input.Sources)
}

func validateFactTombstoneInputBounds(input FactTombstoneInput) error {
	if err := validateFactRuneLimit("subject", input.Subject, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("key", input.Key, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("actor", input.Actor, factAuditMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("reason", input.Reason, factAuditMaxRunes); err != nil {
		return err
	}
	return validateFactSourceBounds(input.Sources)
}

func validateFactClaimBounds(claim *FactClaim) error {
	if claim == nil {
		return nil
	}
	if err := validateFactRuneLimit("claim subject", claim.Subject, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("claim key", claim.Key, factIdentityMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("claim value", claim.Value, factValueMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("claim actor", claim.Actor, factAuditMaxRunes); err != nil {
		return err
	}
	if err := validateFactRuneLimit("claim reason", claim.Reason, factAuditMaxRunes); err != nil {
		return err
	}
	return validateFactSourceBounds(claim.Sources)
}

func validateFactSourceBounds(sources []string) error {
	if len(sources) > factSourcesMax {
		return fmt.Errorf("fact sources exceeds %d entries", factSourcesMax)
	}
	for i, source := range sources {
		if err := validateFactRuneLimit(fmt.Sprintf("source[%d]", i), source, factSourceMaxRunes); err != nil {
			return err
		}
	}
	return nil
}

func validateFactRuneLimit(name, value string, maxRunes int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("fact %s is not valid UTF-8", name)
	}
	if len([]rune(value)) > maxRunes {
		return fmt.Errorf("fact %s exceeds %d runes", name, maxRunes)
	}
	return nil
}

func normalizeFactSubject(subject string) string {
	subject = strings.ToLower(strings.TrimSpace(subject))
	switch subject {
	case "", "self", "me", "operator", "user", "나", "저":
		return "self"
	default:
		return subject
	}
}

func normalizeFactKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = factKeyInvalidRE.ReplaceAllString(key, "-")
	key = strings.Trim(key, "-./:")
	if key == "" {
		return "untitled"
	}
	return key
}

func normalizeFactKind(kind FactKind) FactKind {
	switch kind {
	case FactKindPreference, FactKindIdentity, FactKindAmount, FactKindDeadline, FactKindContract, FactKindSystemState:
		return kind
	default:
		return FactKindGeneric
	}
}

func normalizeFactKindInput(kind FactKind) (FactKind, error) {
	if kind == "" {
		return FactKindGeneric, nil
	}
	if !isKnownFactKind(kind) {
		return "", fmt.Errorf("unknown fact kind %q", kind)
	}
	return kind, nil
}

func isKnownFactKind(kind FactKind) bool {
	switch kind {
	case FactKindGeneric, FactKindPreference, FactKindIdentity, FactKindAmount, FactKindDeadline, FactKindContract, FactKindSystemState:
		return true
	default:
		return false
	}
}

func normalizeFactAuthority(authority FactAuthority) FactAuthority {
	switch authority {
	case FactAuthorityDirectUser, FactAuthorityPrimaryDoc, FactAuthorityRuntime, FactAuthorityAgent, FactAuthorityInference, FactAuthorityLegacyImport:
		return authority
	default:
		return FactAuthorityAgent
	}
}

func normalizeFactAuthorityInput(authority FactAuthority) (FactAuthority, error) {
	if authority == "" {
		return FactAuthorityAgent, nil
	}
	if !isKnownFactAuthority(authority) {
		return "", fmt.Errorf("unknown fact authority %q", authority)
	}
	return authority, nil
}

func isKnownFactAuthority(authority FactAuthority) bool {
	switch authority {
	case FactAuthorityDirectUser, FactAuthorityPrimaryDoc, FactAuthorityRuntime, FactAuthorityAgent, FactAuthorityInference, FactAuthorityLegacyImport:
		return true
	default:
		return false
	}
}

func isKnownFactStatus(status FactStatus) bool {
	switch status {
	case FactStatusCurrent, FactStatusConflicted, FactStatusSuperseded, FactStatusTombstoned:
		return true
	default:
		return false
	}
}

func factIdentity(subject, key string) string {
	return normalizeFactSubject(subject) + ":" + normalizeFactKey(key)
}

func normalizeFactSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	if len(out) > 16 {
		out = out[len(out)-16:]
	}
	return out
}

func factAuthorityRank(kind FactKind, authority FactAuthority) int {
	kind = normalizeFactKind(kind)
	authority = normalizeFactAuthority(authority)
	switch kind {
	case FactKindPreference, FactKindIdentity:
		switch authority {
		case FactAuthorityDirectUser:
			return 500
		case FactAuthorityPrimaryDoc:
			return 350
		case FactAuthorityRuntime:
			return 300
		case FactAuthorityAgent:
			return 250
		case FactAuthorityLegacyImport:
			return 200
		default:
			return 100
		}
	case FactKindAmount, FactKindDeadline, FactKindContract:
		switch authority {
		case FactAuthorityPrimaryDoc:
			return 500
		case FactAuthorityDirectUser:
			return 400
		case FactAuthorityRuntime:
			return 350
		case FactAuthorityAgent:
			return 250
		case FactAuthorityLegacyImport:
			return 200
		default:
			return 100
		}
	case FactKindSystemState:
		switch authority {
		case FactAuthorityRuntime:
			return 500
		case FactAuthorityPrimaryDoc:
			return 400
		case FactAuthorityDirectUser:
			return 350
		case FactAuthorityAgent:
			return 250
		case FactAuthorityLegacyImport:
			return 200
		default:
			return 100
		}
	default:
		switch authority {
		case FactAuthorityDirectUser, FactAuthorityPrimaryDoc, FactAuthorityRuntime:
			return 400
		case FactAuthorityAgent:
			return 250
		case FactAuthorityLegacyImport:
			return 200
		default:
			return 100
		}
	}
}

func factUsesLatestPolicy(kind FactKind, authority FactAuthority) bool {
	switch authority {
	case FactAuthorityDirectUser:
		return kind == FactKindPreference || kind == FactKindIdentity
	case FactAuthorityPrimaryDoc:
		return kind == FactKindAmount || kind == FactKindDeadline || kind == FactKindContract
	case FactAuthorityRuntime:
		return kind == FactKindSystemState
	case FactAuthorityLegacyImport:
		// MEMORY.md and USER.md are an ordered cutover snapshot, not two
		// independent witnesses. The final legacy row for one stable key is
		// therefore the baseline current value instead of a peer conflict.
		return true
	default:
		return false
	}
}

func factRequiresBasisAt(kind FactKind, authority FactAuthority) bool {
	return authority == FactAuthorityPrimaryDoc &&
		(kind == FactKindAmount || kind == FactKindDeadline || kind == FactKindContract)
}

func factBasisTime(claim FactClaim) int64 {
	if claim.BasisAtMs > 0 {
		return claim.BasisAtMs
	}
	return claim.RecordedAtMs
}

func newFactOperationID(revision FactRevision, identity, value string, atMs int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%d", revision, identity, value, atMs)))
	return "fm-" + hex.EncodeToString(sum[:10])
}

func sortedFactIdentities(facts map[string][]FactClaim) []string {
	keys := make([]string, 0, len(facts))
	for key := range facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
