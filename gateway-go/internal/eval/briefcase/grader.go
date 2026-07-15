// Package briefcase contains the deterministic, fail-closed grading core for
// Deneb-Briefcase evaluations. It deliberately has no model or tool runtime
// dependencies: callers provide frozen evidence and a sealed grading plan.
package briefcase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	domainbriefcase "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"golang.org/x/text/unicode/norm"
)

// ReportSchemaVersion identifies the serialized report contract.
const ReportSchemaVersion = "deneb-briefcase-report/v1"

const (
	MinCheckWeight         = 0.000001
	MaxCheckWeight         = 1_000_000
	MaxChecksPerPlanV1     = 256
	MaxCheckIDRunesV1      = 128
	MaxCheckTextRunesV1    = 8_192
	MaxArtifactPathRunesV1 = 1_024
)

// Status is the tri-state result of a check or report. INVALID is distinct
// from FAIL: it means the grader could not produce a trustworthy verdict.
type Status string

const (
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusInvalid Status = "INVALID"
)

// CheckType identifies a deterministic check implementation.
type CheckType string

const (
	CheckExactText      CheckType = "exact_text"
	CheckContains       CheckType = "contains"
	CheckContainsToken  CheckType = "contains_token"
	CheckForbidden      CheckType = "forbidden"
	CheckArtifact       CheckType = "artifact"
	CheckStateJSONEqual CheckType = "state_json_equal"
)

// Check is one sealed rubric item. Fields not used by the selected Type are
// ignored; required fields are validated by the corresponding implementation.
type Check struct {
	ID       string    `json:"id"`
	Type     CheckType `json:"type"`
	Critical bool      `json:"critical,omitempty"`
	Weight   float64   `json:"weight"`

	// ExpectedText is the complete expected text for exact_text.
	ExpectedText string `json:"expectedText,omitempty"`
	// Needle is the literal, case-sensitive substring used by contains and
	// forbidden. An empty needle is invalid rather than vacuously matching.
	Needle string `json:"needle,omitempty"`

	// ArtifactPath is relative to Evidence.ArtifactRoot. ExpectedSHA256 must be
	// a full 64-character SHA-256 digest. Symlinks escaping the root are invalid.
	ArtifactPath   string `json:"artifactPath,omitempty"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`

	// ExpectedState is the sealed JSON value for state_json_equal. Object key
	// order and insignificant number formatting do not affect equality.
	ExpectedState rawJSON `json:"expectedState,omitempty"`
}

// Evidence is the immutable output of one evaluated run.
type Evidence struct {
	Text             string           `json:"text,omitempty"`
	ArtifactRoot     string           `json:"artifactRoot,omitempty"`
	State            rawJSON  `json:"state,omitempty"`
	ArtifactMaxBytes map[string]int64 `json:"-"`
}

// Fingerprint identifies the complete system and case configuration behind a
// report. The fields are intentionally plain strings so the runner can record
// hashes from components this package does not import.
type Fingerprint struct {
	RunID                      string `json:"runId,omitempty"`
	CaseID                     string `json:"caseId"`
	CasepackSHA256             string `json:"casepackSha256,omitempty"`
	Model                      string `json:"model,omitempty"`
	ProviderModel              string `json:"providerModel,omitempty"`
	ToolSchemaSHA256           string `json:"toolSchemaSha256,omitempty"`
	Arm                        string `json:"arm,omitempty"`
	APIMode                    string `json:"apiMode,omitempty"`
	RecallMode                 string `json:"recallMode,omitempty"`
	DevicePlanSHA256           string `json:"devicePlanSha256,omitempty"`
	DevicePlanSourceSHA256     string `json:"devicePlanSourceSha256,omitempty"`
	Seed                       int64  `json:"seed,omitempty"`
	EndpointSHA256             string `json:"endpointSha256,omitempty"`
	BuildSHA256                string `json:"buildSha256,omitempty"`
	ExecutionProfileSHA256     string `json:"executionProfileSha256,omitempty"`
	SystemPromptSequenceSHA256 string `json:"systemPromptSequenceSha256,omitempty"`
}

// Digest returns the SHA-256 of the canonical struct serialization. Struct
// field order is stable, and unlike map serialization cannot vary by caller.
func (f Fingerprint) Digest() string {
	b, err := json.Marshal(f)
	if err != nil {
		// Fingerprint has no field that json.Marshal can reject. Keep this branch
		// fail-closed if that contract changes in the future.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Plan is the sealed set of checks for one run. PassThreshold defaults to 1.0
// when zero and must otherwise be in (0, 1]. Critical failures override it.
type Plan struct {
	Fingerprint   Fingerprint `json:"fingerprint"`
	PassThreshold float64     `json:"passThreshold,omitempty"`
	Checks        []Check     `json:"checks"`
}

// CheckResult is the auditable outcome of one atomic check.
type CheckResult struct {
	ID       string    `json:"id"`
	Type     CheckType `json:"type"`
	Status   Status    `json:"status"`
	Critical bool      `json:"critical,omitempty"`
	Weight   float64   `json:"weight"`
	Detail   string    `json:"detail,omitempty"`
}

// Report is the deterministic result of grading one Plan against Evidence.
type Report struct {
	SchemaVersion     string        `json:"schemaVersion"`
	Status            Status        `json:"status"`
	Score             float64       `json:"score"`
	WeightedPassed    float64       `json:"weightedPassed"`
	WeightedTotal     float64       `json:"weightedTotal"`
	PassThreshold     float64       `json:"passThreshold"`
	CriticalPassed    bool          `json:"criticalPassed"`
	PassedChecks      int           `json:"passedChecks"`
	FailedChecks      int           `json:"failedChecks"`
	InvalidChecks     int           `json:"invalidChecks"`
	Fingerprint       Fingerprint   `json:"fingerprint"`
	FingerprintSHA256 string        `json:"fingerprintSha256"`
	Checks            []CheckResult `json:"checks"`
	Errors            []string      `json:"errors,omitempty"`
}

// Grade evaluates every check even after a failure so the report remains
// diagnostic. Any invalid check or plan makes the whole report INVALID. When
// the plan is valid, a critical failure makes the report FAIL regardless of
// weighted score; otherwise PassThreshold decides PASS versus FAIL.
func Grade(plan Plan, evidence Evidence) Report {
	report, _ := GradeContext(context.Background(), plan, evidence)
	return report
}

// GradeContext is Grade with cooperative cancellation. In particular,
// artifact hashing observes the context between chunks so the closed-loop
// global deadline covers grading as well as executor and simulator work.
func GradeContext(ctx context.Context, plan Plan, evidence Evidence) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	threshold, reportedThreshold, thresholdValid := normalizeGradeThreshold(plan.PassThreshold)
	report := newGradeReport(plan, reportedThreshold)
	if err := ctx.Err(); err != nil {
		return report, err
	}

	grading := newGradeAccumulator(&report, plan, thresholdValid)
	for _, check := range plan.Checks {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := grading.gradeCheck(ctx, check, evidence); err != nil {
			return report, err
		}
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	grading.finalize(threshold, thresholdValid)
	return report, nil
}

func normalizeGradeThreshold(configured float64) (threshold, reported float64, valid bool) {
	threshold = configured
	if threshold == 0 {
		threshold = 1
	}
	valid = threshold > 0 && threshold <= 1 && !math.IsNaN(threshold) && !math.IsInf(threshold, 0)
	reported = threshold
	if math.IsNaN(reported) || math.IsInf(reported, 0) {
		// Reports must remain JSON-serializable even when a malformed plan uses
		// a non-finite number. INVALID carries the verdict; zero is the safe wire
		// representation of the rejected threshold.
		reported = 0
	}
	return threshold, reported, valid
}

func newGradeReport(plan Plan, reportedThreshold float64) Report {
	return Report{
		SchemaVersion:     ReportSchemaVersion,
		Status:            StatusInvalid,
		PassThreshold:     reportedThreshold,
		CriticalPassed:    true,
		Fingerprint:       plan.Fingerprint,
		FingerprintSHA256: plan.Fingerprint.Digest(),
		Checks:            make([]CheckResult, 0, len(plan.Checks)),
	}
}

type gradeAccumulator struct {
	report      *Report
	seen        map[string]struct{}
	exactTotal  *big.Rat
	exactPassed *big.Rat
	planInvalid bool
}

func newGradeAccumulator(report *Report, plan Plan, thresholdValid bool) *gradeAccumulator {
	grading := &gradeAccumulator{
		report:      report,
		seen:        make(map[string]struct{}, len(plan.Checks)),
		exactTotal:  new(big.Rat),
		exactPassed: new(big.Rat),
	}
	if len(plan.Checks) == 0 {
		report.Errors = append(report.Errors, "plan must contain at least one check")
		grading.planInvalid = true
	}
	if len(plan.Checks) > MaxChecksPerPlanV1 {
		report.Errors = append(report.Errors, "plan exceeds the v1 check limit")
		grading.planInvalid = true
	}
	if !thresholdValid {
		report.Errors = append(report.Errors, "pass threshold must be a finite number in (0, 1]")
		grading.planInvalid = true
	}
	return grading
}

func (grading *gradeAccumulator) gradeCheck(ctx context.Context, check Check, evidence Evidence) error {
	result, err := gradeCheckContext(ctx, check, evidence)
	if err != nil {
		return err
	}
	grading.validateCheckID(&result, check.ID)
	if grading.recordCheckWeight(&result, check) {
		grading.report.Checks = append(grading.report.Checks, result)
		return nil
	}
	grading.recordCheckOutcome(&result, check)
	grading.report.Checks = append(grading.report.Checks, result)
	return nil
}

func (grading *gradeAccumulator) validateCheckID(result *CheckResult, rawID string) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		result.Status = StatusInvalid
		result.Detail = "check id is required"
	} else if utf8.RuneCountInString(id) > MaxCheckIDRunesV1 {
		result.Status = StatusInvalid
		result.Detail = "check id exceeds the v1 length limit"
	} else if _, exists := grading.seen[id]; exists {
		result.Status = StatusInvalid
		result.Detail = "duplicate check id"
	}
	grading.seen[id] = struct{}{}
}

// recordCheckWeight returns true when overflow has already finalized the
// check as invalid and no outcome-specific counters should be updated.
func (grading *gradeAccumulator) recordCheckWeight(result *CheckResult, check Check) bool {
	nextTotal := grading.report.WeightedTotal + validWeight(check.Weight)
	if math.IsNaN(nextTotal) || math.IsInf(nextTotal, 0) {
		result.Status = StatusInvalid
		result.Detail = "cumulative check weight exceeds the finite range"
		grading.recordInvalidCheck(check.Critical)
		return true
	}
	grading.report.WeightedTotal = nextTotal
	exactWeight := new(big.Rat)
	if isValidWeight(check.Weight) {
		exactWeight.SetFloat64(check.Weight)
	}
	grading.exactTotal.Add(grading.exactTotal, exactWeight)
	return false
}

func (grading *gradeAccumulator) recordCheckOutcome(result *CheckResult, check Check) {
	switch result.Status {
	case StatusPass:
		nextPassed := grading.report.WeightedPassed + check.Weight
		if math.IsNaN(nextPassed) || math.IsInf(nextPassed, 0) {
			result.Status = StatusInvalid
			result.Detail = "cumulative passed weight exceeds the finite range"
			grading.recordInvalidCheck(check.Critical)
			return
		}
		grading.report.PassedChecks++
		grading.report.WeightedPassed = nextPassed
		exactWeight := new(big.Rat).SetFloat64(check.Weight)
		grading.exactPassed.Add(grading.exactPassed, exactWeight)
	case StatusFail:
		grading.report.FailedChecks++
		if check.Critical {
			grading.report.CriticalPassed = false
		}
	default:
		grading.recordInvalidCheck(check.Critical)
	}
}

func (grading *gradeAccumulator) recordInvalidCheck(critical bool) {
	grading.report.InvalidChecks++
	grading.planInvalid = true
	if critical {
		grading.report.CriticalPassed = false
	}
}

func (grading *gradeAccumulator) finalize(threshold float64, thresholdValid bool) {
	meetsThreshold := false
	if grading.exactTotal.Sign() > 0 && !math.IsNaN(grading.report.WeightedPassed) && !math.IsInf(grading.report.WeightedPassed, 0) {
		exactScore := new(big.Rat).Quo(grading.exactPassed, grading.exactTotal)
		grading.report.Score, _ = exactScore.Float64()
		if thresholdValid {
			thresholdRat := new(big.Rat).SetFloat64(threshold)
			meetsThreshold = exactScore.Cmp(thresholdRat) >= 0
		}
	}
	if math.IsNaN(grading.report.Score) || math.IsInf(grading.report.Score, 0) {
		grading.report.Score = 0
		grading.report.Errors = append(grading.report.Errors, "weighted score is not finite")
		grading.planInvalid = true
	}
	if grading.planInvalid || grading.report.WeightedTotal == 0 {
		grading.report.Status = StatusInvalid
		return
	}
	if !grading.report.CriticalPassed || !meetsThreshold {
		grading.report.Status = StatusFail
		return
	}
	grading.report.Status = StatusPass
}

func gradeCheckContext(ctx context.Context, check Check, evidence Evidence) (CheckResult, error) {
	reportedWeight := check.Weight
	if math.IsNaN(reportedWeight) || math.IsInf(reportedWeight, 0) {
		reportedWeight = 0
	}
	result := CheckResult{
		ID:       check.ID,
		Type:     check.Type,
		Status:   StatusInvalid,
		Critical: check.Critical,
		Weight:   reportedWeight,
	}
	if !isValidWeight(check.Weight) {
		result.Detail = "weight is outside the finite v1 range"
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	switch check.Type {
	case CheckExactText:
		if utf8.RuneCountInString(check.ExpectedText) > MaxCheckTextRunesV1 {
			result.Detail = "expectedText exceeds the v1 length limit"
			return result, nil
		}
		if evidence.Text == check.ExpectedText {
			result.Status = StatusPass
			result.Detail = "text exactly matched"
		} else {
			result.Status = StatusFail
			result.Detail = "text did not exactly match"
		}
	case CheckContains, CheckContainsToken, CheckForbidden:
		if check.Needle == "" {
			result.Detail = "needle is required"
			return result, nil
		}
		if utf8.RuneCountInString(check.Needle) > MaxCheckTextRunesV1 {
			result.Detail = "needle exceeds the v1 length limit"
			return result, nil
		}
		found := strings.Contains(evidence.Text, check.Needle)
		if check.Type == CheckContainsToken {
			found = containsLiteralToken(evidence.Text, check.Needle)
		}
		if check.Type == CheckContains || check.Type == CheckContainsToken {
			if found {
				result.Status = StatusPass
				result.Detail = "required text was present"
			} else {
				result.Status = StatusFail
				result.Detail = "required text was absent"
			}
		} else if found {
			result.Status = StatusFail
			result.Detail = "forbidden text was present"
		} else {
			result.Status = StatusPass
			result.Detail = "forbidden text was absent"
		}
	case CheckArtifact:
		if utf8.RuneCountInString(check.ArtifactPath) > MaxArtifactPathRunesV1 {
			result.Detail = "artifactPath exceeds the v1 length limit"
			return result, nil
		}
		status, detail, err := gradeArtifactContext(ctx, check, evidence)
		if err != nil {
			return result, err
		}
		result.Status, result.Detail = status, detail
	case CheckStateJSONEqual:
		status, detail := gradeState(check.ExpectedState, evidence.State)
		result.Status, result.Detail = status, detail
	default:
		result.Detail = fmt.Sprintf("unsupported check type %q", check.Type)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func containsLiteralToken(text, needle string) bool {
	if needle == "" {
		return false
	}
	numeric := true
	for _, r := range needle {
		if !unicode.IsDigit(r) {
			numeric = false
			break
		}
	}
	for offset := 0; offset <= len(text)-len(needle); {
		relative := strings.Index(text[offset:], needle)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(needle)
		if tokenBoundariesMatch(text, start, end, numeric) {
			return true
		}
		offset = start + 1
	}
	return false
}

func tokenBoundariesMatch(text string, start, end int, numeric bool) bool {
	before, hasBefore := runeBefore(text, start)
	after, hasAfter := runeAfter(text, end)
	before = normalizedBoundaryRune(before)
	after = normalizedBoundaryRune(after)
	if numeric {
		if hasBefore && numericPrefixConflict(text, start, before) {
			return false
		}
		if hasAfter && numericSuffixConflict(text, end, after) {
			return false
		}
		return true
	}
	if hasBefore && isWordRune(before) {
		return false
	}
	return !hasAfter || !isWordRune(after)
}

func numericPrefixConflict(text string, start int, immediate rune) bool {
	if unicode.IsDigit(immediate) || isNumericSign(immediate) {
		return true
	}
	if !isNumericGroupingSeparator(immediate) {
		if immediate == 'x' || immediate == 'X' {
			_, size := utf8.DecodeLastRuneInString(text[:start])
			previous, ok := runeBefore(text, start-size)
			return ok && previous == '0'
		}
		return false
	}
	offset := start
	for offset > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:offset])
		offset -= size
		r = normalizedBoundaryRune(r)
		if isNumericGroupingSeparator(r) {
			continue
		}
		return unicode.IsDigit(r) || isNumericSign(r)
	}
	return false
}

func numericSuffixConflict(text string, end int, immediate rune) bool {
	if unicode.IsDigit(immediate) {
		return true
	}
	if immediate == 'e' || immediate == 'E' {
		offset := end
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
		next, ok := runeAfter(text, offset)
		if ok && isNumericSign(normalizedBoundaryRune(next)) {
			_, signSize := utf8.DecodeRuneInString(text[offset:])
			offset += signSize
		}
		next, ok = runeAfter(text, offset)
		return ok && unicode.IsDigit(next)
	}
	if !isNumericGroupingSeparator(immediate) {
		return false
	}
	offset := end
	for offset < len(text) {
		r, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
		r = normalizedBoundaryRune(r)
		if isNumericGroupingSeparator(r) {
			continue
		}
		return unicode.IsDigit(r)
	}
	return false
}

func isNumericSign(r rune) bool {
	return r == '+' || r == '-' || r == '−'
}

func isNumericGroupingSeparator(r rune) bool {
	return unicode.IsSpace(r) || r == '.' || r == ',' || r == '٫' || r == '٬' || r == '\'' || r == '’'
}

func normalizedBoundaryRune(r rune) rune {
	if r == 0 {
		return r
	}
	normalized := norm.NFKC.String(string(r))
	value, size := utf8.DecodeRuneInString(normalized)
	if value == utf8.RuneError || size != len(normalized) {
		return r
	}
	return value
}

func runeBefore(text string, offset int) (rune, bool) {
	if offset <= 0 || offset > len(text) {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:offset])
	return r, r != utf8.RuneError
}

func runeAfter(text string, offset int) (rune, bool) {
	if offset < 0 || offset >= len(text) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(text[offset:])
	return r, r != utf8.RuneError
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

type artifactGradeRequest struct {
	root           string
	relativePath   string
	expectedSHA256 string
	maxBytes       int64
}

type artifactGradeOutcome struct {
	status Status
	detail string
	err    error
}

func newArtifactGradeOutcome(status Status, detail string) *artifactGradeOutcome {
	return &artifactGradeOutcome{status: status, detail: detail}
}

func canceledArtifactGradeOutcome(err error) *artifactGradeOutcome {
	outcome := newArtifactGradeOutcome(StatusInvalid, "artifact grading was canceled")
	outcome.err = err
	return outcome
}

func (outcome artifactGradeOutcome) result() (Status, string, error) {
	return outcome.status, outcome.detail, outcome.err
}

func gradeArtifactContext(ctx context.Context, check Check, evidence Evidence) (Status, string, error) {
	if outcome := canceledArtifactGrade(ctx); outcome != nil {
		return outcome.result()
	}
	request, outcome := prepareArtifactGradeRequest(check, evidence)
	if outcome != nil {
		return outcome.result()
	}
	resolvedRoot, outcome := inspectArtifactRoot(request.root)
	if outcome != nil {
		return outcome.result()
	}
	target, outcome := inspectArtifactTarget(ctx, resolvedRoot, request.relativePath)
	if outcome != nil {
		return outcome.result()
	}
	if outcome := enforceArtifactSizeLimit(target.info, request.maxBytes); outcome != nil {
		return outcome.result()
	}
	digest, outcome := hashArtifactContext(ctx, target.path, request.maxBytes)
	if outcome != nil {
		return outcome.result()
	}
	return decideArtifactDigest(digest, request.expectedSHA256).result()
}

func canceledArtifactGrade(ctx context.Context) *artifactGradeOutcome {
	if err := ctx.Err(); err != nil {
		return canceledArtifactGradeOutcome(err)
	}
	return nil
}

func prepareArtifactGradeRequest(check Check, evidence Evidence) (artifactGradeRequest, *artifactGradeOutcome) {
	if strings.TrimSpace(evidence.ArtifactRoot) == "" {
		return artifactGradeRequest{}, newArtifactGradeOutcome(StatusInvalid, "artifact root is required")
	}
	relativePath, ok := normalizeArtifactRelativePath(check.ArtifactPath)
	if !ok {
		return artifactGradeRequest{}, newArtifactGradeOutcome(StatusInvalid, "artifact path must be a safe relative file path")
	}
	expectedSHA256, ok := normalizeArtifactDigest(check.ExpectedSHA256)
	if !ok {
		return artifactGradeRequest{}, newArtifactGradeOutcome(StatusInvalid, "expected artifact sha256 must be a full hexadecimal digest")
	}
	maxBytes := int64(domainbriefcase.MaxArtifactBytesV1)
	if signed, exists := evidence.ArtifactMaxBytes[filepath.ToSlash(relativePath)]; exists && signed > 0 {
		maxBytes = signed
	}
	return artifactGradeRequest{
		root:           evidence.ArtifactRoot,
		relativePath:   relativePath,
		expectedSHA256: expectedSHA256,
		maxBytes:       maxBytes,
	}, nil
}

func normalizeArtifactRelativePath(path string) (string, bool) {
	rawPath := strings.TrimSpace(path)
	relativePath := filepath.Clean(rawPath)
	if relativePath == "." || relativePath == "" || relativePath != rawPath || strings.ContainsRune(relativePath, '\x00') {
		return "", false
	}
	if filepath.IsAbs(relativePath) || filepath.VolumeName(relativePath) != "" {
		return "", false
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relativePath, true
}

func normalizeArtifactDigest(digest string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(digest))
	decoded, err := hex.DecodeString(normalized)
	if err != nil || len(decoded) != sha256.Size || len(normalized) != sha256.Size*2 {
		return "", false
	}
	return normalized, true
}

func inspectArtifactRoot(root string) (string, *artifactGradeOutcome) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact root could not be resolved")
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", newArtifactGradeOutcome(StatusFail, "artifact root does not exist")
		}
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact root could not be inspected")
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact root is not a directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact root could not be resolved")
	}
	return resolvedRoot, nil
}

type inspectedArtifact struct {
	path string
	info os.FileInfo
}

// inspectArtifactTarget uses Lstat for every component so the grader rejects a
// symlink instead of following it outside the sealed artifact root.
func inspectArtifactTarget(ctx context.Context, root, relativePath string) (inspectedArtifact, *artifactGradeOutcome) {
	targetPath := root
	parts := strings.Split(relativePath, string(filepath.Separator))
	var targetInfo os.FileInfo
	for index, part := range parts {
		if outcome := canceledArtifactGrade(ctx); outcome != nil {
			return inspectedArtifact{}, outcome
		}
		targetPath = filepath.Join(targetPath, part)
		terminal := index == len(parts)-1
		info, outcome := inspectArtifactPathComponent(targetPath, terminal)
		if outcome != nil {
			return inspectedArtifact{}, outcome
		}
		if terminal {
			targetInfo = info
		}
	}
	return inspectedArtifact{path: targetPath, info: targetInfo}, nil
}

func inspectArtifactPathComponent(path string, terminal bool) (os.FileInfo, *artifactGradeOutcome) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newArtifactGradeOutcome(StatusFail, "artifact does not exist")
		}
		return nil, newArtifactGradeOutcome(StatusInvalid, "artifact could not be inspected")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, newArtifactGradeOutcome(StatusInvalid, "artifact path contains a symlink")
	}
	if !terminal && !info.IsDir() {
		return nil, newArtifactGradeOutcome(StatusFail, "artifact path parent is not a directory")
	}
	if terminal && !info.Mode().IsRegular() {
		return nil, newArtifactGradeOutcome(StatusFail, "artifact is not a regular file")
	}
	return info, nil
}

func enforceArtifactSizeLimit(info os.FileInfo, maxBytes int64) *artifactGradeOutcome {
	if info == nil || info.Size() > maxBytes {
		return newArtifactGradeOutcome(StatusInvalid, "artifact exceeds its signed size limit")
	}
	return nil
}

func hashArtifactContext(ctx context.Context, path string, maxBytes int64) (string, *artifactGradeOutcome) {
	file, err := os.Open(path)
	if err != nil {
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact could not be opened")
	}
	defer file.Close()

	// The limited reader rechecks the signed bound while hashing, closing the
	// gap where a file grows after the preceding size inspection.
	hash := sha256.New()
	limited := &io.LimitedReader{R: file, N: maxBytes + 1}
	if err := hashReaderContext(ctx, hash, limited); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", canceledArtifactGradeOutcome(err)
		}
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact could not be hashed")
	}
	if limited.N <= 0 {
		return "", newArtifactGradeOutcome(StatusInvalid, "artifact exceeds its signed size limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func decideArtifactDigest(actual, expected string) *artifactGradeOutcome {
	if actual != expected {
		return newArtifactGradeOutcome(StatusFail, "artifact sha256 did not match")
	}
	return newArtifactGradeOutcome(StatusPass, "artifact exists and sha256 matched")
}

func hashReaderContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			if _, err := dst.Write(buffer[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return ctx.Err()
		}
		if readErr != nil {
			return readErr
		}
	}
}

func gradeState(expected, actual json.RawMessage) (Status, string) {
	if len(bytes.TrimSpace(expected)) == 0 {
		return StatusInvalid, "expected state JSON is required"
	}
	if len(bytes.TrimSpace(actual)) == 0 {
		return StatusInvalid, "actual state JSON is required"
	}
	want, err := decodeOneJSON(expected)
	if err != nil {
		return StatusInvalid, "expected state JSON is invalid"
	}
	got, err := decodeOneJSON(actual)
	if err != nil {
		return StatusInvalid, "actual state JSON is invalid"
	}
	if semanticJSONEqual(want, got) {
		return StatusPass, "state JSON matched"
	}
	return StatusFail, "state JSON did not match"
}

func decodeOneJSON(raw []byte) (any, error) {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := validateBoundedJSONNumbers(value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func validateBoundedJSONNumbers(value any) error {
	switch typed := value.(type) {
	case json.Number:
		if _, ok := domainbriefcase.ParseBoundedRational(typed.String()); !ok {
			return errors.New("JSON number exceeds the deterministic v1 bounds")
		}
	case []any:
		for _, child := range typed {
			if err := validateBoundedJSONNumbers(child); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, child := range typed {
			if err := validateBoundedJSONNumbers(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectDuplicateJSONKeys prevents ambiguous state such as
// {"approved":false,"approved":true} from being accepted according to the
// decoder's last-key-wins behavior.
func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := walkJSONValue(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func walkJSONValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("object was not closed")
		}
	case '[':
		for dec.More() {
			if err := walkJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("array was not closed")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func semanticJSONEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, aok := domainbriefcase.ParseBoundedRational(string(av))
		br, bok := domainbriefcase.ParseBoundedRational(string(bv))
		return aok && bok && ar.Cmp(br) == 0
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !semanticJSONEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for key, value := range av {
			other, exists := bv[key]
			if !exists || !semanticJSONEqual(value, other) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func isValidWeight(weight float64) bool {
	return weight >= MinCheckWeight && weight <= MaxCheckWeight && !math.IsNaN(weight) && !math.IsInf(weight, 0)
}

func validWeight(weight float64) float64 {
	if !isValidWeight(weight) {
		return 0
	}
	return weight
}
