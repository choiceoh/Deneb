package feedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const FeedbackSchemaVersion = "deneb-briefcase-feedback/v1"

const (
	defaultMaxFollowUpRunes   = 2_000
	defaultMaxSummaryRunes    = 500
	defaultMaxLabelRunes      = 128
	defaultMaxTrajectoryItems = 24
	defaultMaxArtifactItems   = 48

	hardMaxFollowUpRunes    = 8_192
	hardMaxSummaryRunes     = 2_048
	hardMaxLabelRunes       = 256
	hardMaxTrajectoryItems  = 128
	hardMaxArtifactItems    = 256
	hardMaxHiddenTokens     = 4_096
	hardMaxHiddenTokenRunes = 8_192
	maxFeedbackMessagesV1   = 2
	maxSealedFeedbackBytes  = 16 << 20
)

var (
	ErrInvalidFeedback = errors.New("invalid briefcase feedback")
	ErrFeedbackLeak    = errors.New("briefcase feedback contains hidden supervisor information")
)

// VerdictCategory is intentionally coarser than a sealed grader report. It
// cannot carry check IDs, failure details, or supervisor reasoning.
type VerdictCategory string

const (
	VerdictSatisfactory  VerdictCategory = "satisfactory"
	VerdictNeedsRevision VerdictCategory = "needs-revision"
	VerdictBlocked       VerdictCategory = "blocked"
	VerdictCannotAssess  VerdictCategory = "cannot-assess"
)

// ScoreBand discloses no exact score or threshold.
type ScoreBand string

const (
	ScoreBandHigh        ScoreBand = "high"
	ScoreBandMedium      ScoreBand = "medium"
	ScoreBandLow         ScoreBand = "low"
	ScoreBandUnavailable ScoreBand = "unavailable"
)

// ArtifactSummaryStatus describes only an artifact state observable from the
// public run trajectory.
type ArtifactSummaryStatus string

const (
	ArtifactAvailable  ArtifactSummaryStatus = "available"
	ArtifactMissing    ArtifactSummaryStatus = "missing"
	ArtifactUnreadable ArtifactSummaryStatus = "unreadable"
)

// HiddenFeedbackInputs are supervisor-only values used solely as deny tokens.
// FeedbackFirewall deliberately provides no method that returns them.
type HiddenFeedbackInputs struct {
	SealedSourceIDs         []string
	SealedPaths             []string
	HiddenReferences        []string
	RubricIDs               []string
	CheckpointIDs           []string
	SupervisorReasoning     []string
	HiddenRationales        []string
	ExpectedAnswers         []string
	SupervisorMetadata      []string
	SealedContents          []string
	ExplicitSensitiveTokens []string
}

// FeedbackLimits are bounded even when caller-supplied. Zero selects the safe
// default; negative or above-hard-cap values are rejected.
type FeedbackLimits struct {
	MaxFollowUpRunes   int
	MaxSummaryRunes    int
	MaxLabelRunes      int
	MaxTrajectoryItems int
	MaxArtifactItems   int
}

// VisibleArtifactSummary contains no path, digest, expected value, or hidden
// reference. Label must be a public artifact label.
type VisibleArtifactSummary struct {
	Label   string                `json:"label"`
	Status  ArtifactSummaryStatus `json:"status"`
	Summary string                `json:"summary"`
}

// SimulatorHandoffInput is the complete public input accepted by the firewall.
// There is intentionally no field for a rubric, hidden reference, expected
// answer, check result, or chain-of-thought.
type SimulatorHandoffInput struct {
	VerdictCategory            VerdictCategory
	Recoverable                bool
	ScoreBand                  ScoreBand
	VisibleTrajectorySummaries []string
	VisibleArtifactSummaries   []VisibleArtifactSummary
}

// SimulatorHandoff is construction-locked: fields are private and the zero
// value refuses JSON serialization. Use FeedbackFirewall.BuildHandoff.
type SimulatorHandoff struct {
	valid           bool
	verdictCategory VerdictCategory
	recoverable     bool
	scoreBand       ScoreBand
	trajectory      []string
	artifacts       []VisibleArtifactSummary
}

type handoffWire struct {
	SchemaVersion              string                   `json:"schemaVersion"`
	VerdictCategory            VerdictCategory          `json:"verdictCategory"`
	Recoverable                bool                     `json:"recoverable"`
	ScoreBand                  ScoreBand                `json:"scoreBand"`
	VisibleTrajectorySummaries []string                 `json:"visibleTrajectorySummaries"`
	VisibleArtifactSummaries   []VisibleArtifactSummary `json:"visibleArtifactSummaries,omitempty"`
}

func (h SimulatorHandoff) VerdictCategory() VerdictCategory { return h.verdictCategory }
func (h SimulatorHandoff) Recoverable() bool                { return h.recoverable }
func (h SimulatorHandoff) ScoreBand() ScoreBand             { return h.scoreBand }

func (h SimulatorHandoff) VisibleTrajectorySummaries() []string {
	return append([]string(nil), h.trajectory...)
}

func (h SimulatorHandoff) VisibleArtifactSummaries() []VisibleArtifactSummary {
	return append([]VisibleArtifactSummary(nil), h.artifacts...)
}

func (h SimulatorHandoff) MarshalJSON() ([]byte, error) {
	if !h.valid {
		return nil, validationError("handoff", "must be constructed by FeedbackFirewall")
	}
	return json.Marshal(handoffWire{
		SchemaVersion:              FeedbackSchemaVersion,
		VerdictCategory:            h.verdictCategory,
		Recoverable:                h.recoverable,
		ScoreBand:                  h.scoreBand,
		VisibleTrajectorySummaries: append([]string(nil), h.trajectory...),
		VisibleArtifactSummaries:   append([]VisibleArtifactSummary(nil), h.artifacts...),
	})
}

type ForbiddenFeedbackClass string

const (
	ForbiddenSealedSource        ForbiddenFeedbackClass = "sealed-source"
	ForbiddenSealedPath          ForbiddenFeedbackClass = "sealed-path"
	ForbiddenHiddenReference     ForbiddenFeedbackClass = "hidden-reference"
	ForbiddenRubricID            ForbiddenFeedbackClass = "rubric-id"
	ForbiddenCheckpointID        ForbiddenFeedbackClass = "checkpoint-id"
	ForbiddenSupervisorReasoning ForbiddenFeedbackClass = "supervisor-reasoning"
	ForbiddenHiddenRationale     ForbiddenFeedbackClass = "hidden-rationale"
	ForbiddenExpectedAnswer      ForbiddenFeedbackClass = "expected-answer"
	ForbiddenSupervisorMetadata  ForbiddenFeedbackClass = "supervisor-metadata"
	ForbiddenExplicitSensitive   ForbiddenFeedbackClass = "explicit-sensitive-token"
	ForbiddenStructuralMarker    ForbiddenFeedbackClass = "supervisor-structural-marker"
)

type FeedbackLeakError struct {
	Class ForbiddenFeedbackClass
}

func (e *FeedbackLeakError) Error() string {
	if e == nil {
		return ErrFeedbackLeak.Error()
	}
	// Never echo the matching token or candidate text in an error: errors can be
	// logged or accidentally surfaced to the simulator.
	return fmt.Sprintf("%s: class=%s", ErrFeedbackLeak, e.Class)
}

func (e *FeedbackLeakError) Unwrap() error { return ErrFeedbackLeak }

type feedbackValidationError struct {
	field  string
	reason string
}

func (e *feedbackValidationError) Error() string {
	return fmt.Sprintf("%s: field=%s reason=%s", ErrInvalidFeedback, e.field, e.reason)
}

func (e *feedbackValidationError) Unwrap() error { return ErrInvalidFeedback }

type forbiddenFeedbackToken struct {
	class      ForbiddenFeedbackClass
	normalized string
	compact    string
	numeric    string
	fragments  []string
}

// FeedbackFirewall holds supervisor tokens only for negative matching. Its
// String and GoString methods reveal counts, never token values.
type FeedbackFirewall struct {
	limits            FeedbackLimits
	forbidden         []forbiddenFeedbackToken
	mu                sync.Mutex
	historyNormalized string
	historyCompact    string
	fragmentSeen      []map[string]struct{}
	acceptedMessages  int
}

func (f *FeedbackFirewall) String() string {
	if f == nil {
		return "FeedbackFirewall<nil>"
	}
	return fmt.Sprintf("FeedbackFirewall{forbidden:%d}", len(f.forbidden))
}

func (f *FeedbackFirewall) GoString() string { return f.String() }

func NewFeedbackFirewall(hidden HiddenFeedbackInputs, limits FeedbackLimits) (*FeedbackFirewall, error) {
	resolved, err := resolveFeedbackLimits(limits)
	if err != nil {
		return nil, err
	}
	tokenGroups := []struct {
		class  ForbiddenFeedbackClass
		values []string
	}{
		{ForbiddenSealedSource, hidden.SealedSourceIDs},
		{ForbiddenSealedPath, hidden.SealedPaths},
		{ForbiddenHiddenReference, hidden.HiddenReferences},
		{ForbiddenRubricID, hidden.RubricIDs},
		{ForbiddenCheckpointID, hidden.CheckpointIDs},
		{ForbiddenSupervisorReasoning, hidden.SupervisorReasoning},
		{ForbiddenHiddenRationale, hidden.HiddenRationales},
		{ForbiddenExpectedAnswer, hidden.ExpectedAnswers},
		{ForbiddenSupervisorMetadata, hidden.SupervisorMetadata},
		{ForbiddenExplicitSensitive, hidden.ExplicitSensitiveTokens},
	}
	count := 0
	for _, group := range tokenGroups {
		count += len(group.values)
	}
	if count > hardMaxHiddenTokens {
		return nil, validationError("hiddenInputs", "too many deny tokens")
	}

	seen := make(map[string]struct{}, count+len(structuralFeedbackMarkers))
	forbidden := make([]forbiddenFeedbackToken, 0, count+len(structuralFeedbackMarkers))
	add := func(class ForbiddenFeedbackClass, value string) error {
		if !utf8.ValidString(value) {
			return validationError("hiddenInputs", "deny token is not valid UTF-8")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return validationError("hiddenInputs", "deny token is empty")
		}
		if utf8.RuneCountInString(value) > hardMaxHiddenTokenRunes {
			return validationError("hiddenInputs", "deny token exceeds hard limit")
		}
		normalized := normalizeForLeakScan(value)
		if normalized == "" {
			return validationError("hiddenInputs", "deny token is empty after normalization")
		}
		key := string(class) + "\x00" + normalized
		if _, exists := seen[key]; exists {
			return nil
		}
		seen[key] = struct{}{}
		fragments := []string(nil)
		if expandsLeakFragments(class) {
			fragments = leakFragments(normalized)
		}
		forbidden = append(forbidden, forbiddenFeedbackToken{
			class: class, normalized: normalized, compact: compactForLeakScan(value),
			numeric: canonicalLeakNumber(normalized), fragments: fragments,
		})
		if len(forbidden) > hardMaxHiddenTokens {
			return validationError("hiddenInputs", "too many expanded deny tokens")
		}
		return nil
	}
	for _, group := range tokenGroups {
		for _, value := range group.values {
			if err := add(group.class, value); err != nil {
				return nil, err
			}
			if !expandsLeakFragments(group.class) {
				continue
			}
			for _, fragment := range leakFragments(value) {
				if !isSensitiveLeakFragment(group.class, fragment) {
					continue
				}
				if err := add(group.class, fragment); err != nil {
					return nil, err
				}
			}
		}
	}
	totalSealedBytes := 0
	for _, content := range hidden.SealedContents {
		if !utf8.ValidString(content) {
			return nil, validationError("hiddenInputs", "sealed content is not valid UTF-8")
		}
		totalSealedBytes += len(content)
		if totalSealedBytes > maxSealedFeedbackBytes {
			return nil, validationError("hiddenInputs", "sealed text exceeds the scan limit")
		}
		if utf8.RuneCountInString(content) <= hardMaxHiddenTokenRunes {
			if err := add(ForbiddenSealedSource, content); err != nil {
				return nil, err
			}
		}
		for _, fragment := range leakFragments(content) {
			if !isSensitiveLeakFragment(ForbiddenSealedSource, fragment) {
				continue
			}
			if err := add(ForbiddenSealedSource, fragment); err != nil {
				return nil, err
			}
		}
	}
	for _, marker := range structuralFeedbackMarkers {
		if err := add(ForbiddenStructuralMarker, marker); err != nil {
			return nil, err
		}
	}
	sort.Slice(forbidden, func(i, j int) bool {
		// Specific supervisor-provided tokens take precedence over generic
		// structural markers, and longer tokens take precedence over their
		// prefixes. This keeps leak classification deterministic and useful
		// without ever exposing the matching token itself.
		leftStructural := forbidden[i].class == ForbiddenStructuralMarker
		rightStructural := forbidden[j].class == ForbiddenStructuralMarker
		if leftStructural != rightStructural {
			return !leftStructural
		}
		if len(forbidden[i].normalized) != len(forbidden[j].normalized) {
			return len(forbidden[i].normalized) > len(forbidden[j].normalized)
		}
		if forbidden[i].normalized != forbidden[j].normalized {
			return forbidden[i].normalized < forbidden[j].normalized
		}
		return forbidden[i].class < forbidden[j].class
	})
	return &FeedbackFirewall{
		limits: resolved, forbidden: forbidden, fragmentSeen: make([]map[string]struct{}, len(forbidden)),
	}, nil
}

var structuralFeedbackMarkers = []string{
	"sealed/", "sealed:", "sealed-", "sealed_",
	"rubric:", "rubric_id", "rubric-id", "rubricid",
	"checkpoint:", "checkpoint_id", "checkpoint-id", "checkpointid",
	"hidden rationale", "hidden_rationale", "hidden-rationale",
	"expected answer", "expected_answer", "expected-answer",
	"supervisor reasoning", "supervisor_reasoning", "supervisor-reasoning",
	"pass threshold", "pass_threshold", "pass-threshold", "passthreshold", "threshold",
	"check weight", "check_weight", "check-weight", "weight",
	"check type", "check_type", "check-type",
	"critical check", "critical_check", "critical-check", "critical",
	"plan digest", "plan_digest", "plan-digest",
	"source digest", "source_digest", "source-digest",
	"fingerprint", "max cycles", "max_cycles", "max-cycles", "maxcycles",
	"passing score", "required score", "score",
}

func (f *FeedbackFirewall) SanitizeFollowUp(text string) (string, error) {
	if f == nil {
		return "", validationError("firewall", "is required")
	}
	return f.validateText("followUpText", text, f.limits.MaxFollowUpRunes, true)
}

// BuildHandoff constructs the one-way supervisor -> user-simulator message. It
// deliberately cannot carry simulator output. After the simulator responds,
// pass that separate string through SanitizeFollowUp before executor delivery.
func (f *FeedbackFirewall) BuildHandoff(input SimulatorHandoffInput) (SimulatorHandoff, error) {
	if f == nil {
		return SimulatorHandoff{}, validationError("firewall", "is required")
	}
	if !validVerdictCategory(input.VerdictCategory) {
		return SimulatorHandoff{}, validationError("verdictCategory", "is unsupported")
	}
	if !validScoreBand(input.ScoreBand) {
		return SimulatorHandoff{}, validationError("scoreBand", "is unsupported")
	}
	if len(input.VisibleTrajectorySummaries) == 0 {
		return SimulatorHandoff{}, validationError("visibleTrajectorySummaries", "must not be empty")
	}
	if len(input.VisibleTrajectorySummaries) > f.limits.MaxTrajectoryItems {
		return SimulatorHandoff{}, validationError("visibleTrajectorySummaries", "exceeds item limit")
	}
	if len(input.VisibleArtifactSummaries) > f.limits.MaxArtifactItems {
		return SimulatorHandoff{}, validationError("visibleArtifactSummaries", "exceeds item limit")
	}

	trajectory := make([]string, len(input.VisibleTrajectorySummaries))
	for i, summary := range input.VisibleTrajectorySummaries {
		clean, err := f.validateText("visibleTrajectorySummaries", summary, f.limits.MaxSummaryRunes, false)
		if err != nil {
			return SimulatorHandoff{}, err
		}
		trajectory[i] = clean
	}

	artifacts := make([]VisibleArtifactSummary, len(input.VisibleArtifactSummaries))
	seenLabels := make(map[string]struct{}, len(artifacts))
	for i, artifact := range input.VisibleArtifactSummaries {
		if !validArtifactStatus(artifact.Status) {
			return SimulatorHandoff{}, validationError("visibleArtifactSummaries.status", "is unsupported")
		}
		label, err := f.validateText("visibleArtifactSummaries.label", artifact.Label, f.limits.MaxLabelRunes, false)
		if err != nil {
			return SimulatorHandoff{}, err
		}
		summary, err := f.validateText("visibleArtifactSummaries.summary", artifact.Summary, f.limits.MaxSummaryRunes, false)
		if err != nil {
			return SimulatorHandoff{}, err
		}
		labelKey := strings.ToLower(label)
		if _, duplicate := seenLabels[labelKey]; duplicate {
			return SimulatorHandoff{}, validationError("visibleArtifactSummaries.label", "is duplicated")
		}
		seenLabels[labelKey] = struct{}{}
		artifacts[i] = VisibleArtifactSummary{Label: label, Status: artifact.Status, Summary: summary}
	}
	// Artifacts have no trajectory ordering, so canonicalize them for stable JSON
	// even if a caller collected them from a map.
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Label != artifacts[j].Label {
			return artifacts[i].Label < artifacts[j].Label
		}
		return artifacts[i].Status < artifacts[j].Status
	})

	return SimulatorHandoff{
		valid:           true,
		verdictCategory: input.VerdictCategory,
		recoverable:     input.Recoverable,
		scoreBand:       input.ScoreBand,
		trajectory:      trajectory,
		artifacts:       artifacts,
	}, nil
}

func (f *FeedbackFirewall) validateText(field, text string, maxRunes int, scanHidden bool) (string, error) {
	if !utf8.ValidString(text) {
		return "", validationError(field, "must be valid UTF-8")
	}
	text = canonicalFeedbackText(text)
	if text == "" {
		return "", validationError(field, "must not be empty")
	}
	if utf8.RuneCountInString(text) > maxRunes {
		return "", validationError(field, "exceeds length limit")
	}
	for _, r := range text {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return "", validationError(field, "contains a disallowed control character")
		}
	}
	if scanHidden {
		if err := f.scanAndCommit(text); err != nil {
			return "", err
		}
	}
	return text, nil
}

func (f *FeedbackFirewall) scanAndCommit(text string) error {
	normalized := normalizeForLeakScan(text)
	compact := compactForLeakScan(text)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acceptedMessages >= maxFeedbackMessagesV1 {
		return validationError("followUpText", "exceeds the v1 cumulative message limit")
	}
	combinedNormalized := f.historyNormalized + normalized
	combinedCompact := f.historyCompact + compact
	if utf8.RuneCountInString(combinedNormalized) > hardMaxFollowUpRunes*maxFeedbackMessagesV1 {
		return validationError("followUpText", "exceeds the v1 cumulative length limit")
	}
	numerics := numericLeakTokens(combinedNormalized)
	proposedSeen := make([]map[string]struct{}, len(f.forbidden))
	for index, token := range f.forbidden {
		compactMatch := utf8.RuneCountInString(token.compact) >= 2 && strings.Contains(combinedCompact, token.compact)
		_, numericMatch := numerics[token.numeric]
		if token.numeric == "" {
			numericMatch = false
		}
		seen := cloneStringSet(f.fragmentSeen[index])
		for _, fragment := range token.fragments {
			if strings.Contains(compact, fragment) {
				if seen == nil {
					seen = make(map[string]struct{}, len(token.fragments))
				}
				seen[fragment] = struct{}{}
			}
		}
		fragmentMatch := len(token.fragments) >= 2 && len(seen) == len(token.fragments)
		if strings.Contains(combinedNormalized, token.normalized) || compactMatch || numericMatch || fragmentMatch {
			return &FeedbackLeakError{Class: token.class}
		}
		proposedSeen[index] = seen
	}
	f.historyNormalized = combinedNormalized
	f.historyCompact = combinedCompact
	f.fragmentSeen = proposedSeen
	f.acceptedMessages++
	return nil
}

func cloneStringSet(source map[string]struct{}) map[string]struct{} {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]struct{}, len(source))
	for value := range source {
		clone[value] = struct{}{}
	}
	return clone
}

func canonicalFeedbackText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

func normalizeForLeakScan(text string) string {
	text = norm.NFKD.String(text)
	var normalized strings.Builder
	for _, r := range text {
		if unicode.In(r, unicode.Cf) || unicode.Is(unicode.M, r) {
			continue
		}
		if digit, ok := decimalDigitASCII(r); ok {
			normalized.WriteRune(digit)
			continue
		}
		normalized.WriteRune(unicode.ToLower(r))
	}
	text = cases.Fold().String(normalized.String())
	text = norm.NFKC.String(text)
	text = strings.ReplaceAll(text, "\\", "/")
	return strings.Join(strings.Fields(text), " ")
}

func compactForLeakScan(text string) string {
	text = normalizeForLeakScan(text)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, text)
}

func decimalDigitASCII(r rune) (rune, bool) {
	if r >= '0' && r <= '9' {
		return r, true
	}
	value := uint32(r)
	for _, item := range unicode.Digit.R16 {
		lo, hi, stride := uint32(item.Lo), uint32(item.Hi), uint32(item.Stride)
		if value >= lo && value <= hi && (value-lo)%stride == 0 {
			return '0' + rune(((value-lo)/stride)%10), true
		}
	}
	for _, item := range unicode.Digit.R32 {
		lo, hi, stride := item.Lo, item.Hi, item.Stride
		if value >= lo && value <= hi && (value-lo)%stride == 0 {
			return '0' + rune(((value-lo)/stride)%10), true
		}
	}
	return 0, false
}

const leakDecimalAtom = `[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`

var (
	leakNumberPattern        = regexp.MustCompile(leakDecimalAtom + `(?:\s*/\s*` + leakDecimalAtom + `)?%?`)
	groupedLeakNumberPattern = regexp.MustCompile(`[+-]?[0-9]{1,3}(?:[,'’][0-9]{3})+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?%?`)
	spacedLeakNumberPattern  = regexp.MustCompile(`[+-]?[0-9]{1,3}(?:\s+[0-9]{3})+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?%?`)
)

func canonicalLeakNumber(text string) string {
	text = strings.TrimSpace(text)
	if groupedLeakNumberPattern.FindString(text) == text {
		text = strings.NewReplacer(",", "", "'", "", "’", "").Replace(text)
	} else if spacedLeakNumberPattern.FindString(text) == text {
		text = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, text)
	} else if leakNumberPattern.FindString(text) != text {
		return ""
	}
	canonical, ok := casepack.BoundedRationalKey(text)
	if !ok {
		return ""
	}
	return canonical
}

func numericLeakTokens(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, pattern := range []*regexp.Regexp{leakNumberPattern, groupedLeakNumberPattern, spacedLeakNumberPattern} {
		for _, match := range pattern.FindAllString(text, -1) {
			if canonical := canonicalLeakNumber(match); canonical != "" {
				tokens[canonical] = struct{}{}
			}
		}
	}
	return tokens
}

func leakFragments(text string) []string {
	text = normalizeForLeakScan(text)
	var fragments []string
	var current strings.Builder
	currentDigit := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		fragments = append(fragments, current.String())
		current.Reset()
	}
	for _, r := range text {
		isDigit := r >= '0' && r <= '9'
		if !unicode.IsLetter(r) && !isDigit {
			flush()
			continue
		}
		if current.Len() > 0 && isDigit != currentDigit {
			flush()
		}
		currentDigit = isDigit
		current.WriteRune(r)
	}
	flush()
	seen := make(map[string]struct{}, len(fragments))
	out := fragments[:0]
	for _, fragment := range fragments {
		if _, exists := seen[fragment]; exists {
			continue
		}
		seen[fragment] = struct{}{}
		out = append(out, fragment)
	}
	return out
}

func expandsLeakFragments(class ForbiddenFeedbackClass) bool {
	switch class {
	case ForbiddenSealedSource, ForbiddenSealedPath, ForbiddenHiddenReference, ForbiddenRubricID,
		ForbiddenCheckpointID, ForbiddenSupervisorReasoning, ForbiddenHiddenRationale,
		ForbiddenExpectedAnswer, ForbiddenExplicitSensitive:
		return true
	default:
		return false
	}
}

func isSensitiveLeakFragment(class ForbiddenFeedbackClass, fragment string) bool {
	if canonicalLeakNumber(fragment) != "" {
		return true
	}
	runeCount := utf8.RuneCountInString(fragment)
	if _, common := leakFragmentStopwords[fragment]; common {
		return false
	}
	if class == ForbiddenExpectedAnswer && runeCount >= 3 {
		return true
	}
	if runeCount >= 3 {
		return true
	}
	if runeCount < 2 {
		return false
	}
	for _, r := range fragment {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

var leakFragmentStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {},
	"are": {}, "was": {}, "were": {}, "has": {}, "have": {}, "had": {}, "not": {},
	"but": {}, "can": {}, "use": {}, "code": {}, "answer": {}, "value": {}, "check": {},
	"type": {}, "weight": {}, "score": {}, "plan": {}, "budget": {}, "approved": {},
	"latest": {}, "project": {},
}

func validVerdictCategory(category VerdictCategory) bool {
	switch category {
	case VerdictSatisfactory, VerdictNeedsRevision, VerdictBlocked, VerdictCannotAssess:
		return true
	default:
		return false
	}
}

func validScoreBand(band ScoreBand) bool {
	switch band {
	case ScoreBandHigh, ScoreBandMedium, ScoreBandLow, ScoreBandUnavailable:
		return true
	default:
		return false
	}
}

func validArtifactStatus(status ArtifactSummaryStatus) bool {
	switch status {
	case ArtifactAvailable, ArtifactMissing, ArtifactUnreadable:
		return true
	default:
		return false
	}
}

func resolveFeedbackLimits(limits FeedbackLimits) (FeedbackLimits, error) {
	values := []struct {
		field        string
		value        *int
		defaultValue int
		hardMax      int
	}{
		{"maxFollowUpRunes", &limits.MaxFollowUpRunes, defaultMaxFollowUpRunes, hardMaxFollowUpRunes},
		{"maxSummaryRunes", &limits.MaxSummaryRunes, defaultMaxSummaryRunes, hardMaxSummaryRunes},
		{"maxLabelRunes", &limits.MaxLabelRunes, defaultMaxLabelRunes, hardMaxLabelRunes},
		{"maxTrajectoryItems", &limits.MaxTrajectoryItems, defaultMaxTrajectoryItems, hardMaxTrajectoryItems},
		{"maxArtifactItems", &limits.MaxArtifactItems, defaultMaxArtifactItems, hardMaxArtifactItems},
	}
	for _, item := range values {
		if *item.value < 0 || *item.value > item.hardMax {
			return FeedbackLimits{}, validationError(item.field, "is outside the safe range")
		}
		if *item.value == 0 {
			*item.value = item.defaultValue
		}
	}
	return limits, nil
}

func validationError(field, reason string) error {
	return &feedbackValidationError{field: field, reason: reason}
}
