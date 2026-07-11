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
	ExpectedState json.RawMessage `json:"expectedState,omitempty"`
}

// Evidence is the immutable output of one evaluated run.
type Evidence struct {
	Text             string           `json:"text,omitempty"`
	ArtifactRoot     string           `json:"artifactRoot,omitempty"`
	State            json.RawMessage  `json:"state,omitempty"`
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
	threshold := plan.PassThreshold
	if threshold == 0 {
		threshold = 1
	}
	thresholdValid := threshold > 0 && threshold <= 1 && !math.IsNaN(threshold) && !math.IsInf(threshold, 0)
	reportedThreshold := threshold
	if math.IsNaN(reportedThreshold) || math.IsInf(reportedThreshold, 0) {
		// Reports must remain JSON-serializable even when a malformed plan uses
		// a non-finite number. INVALID carries the verdict; zero is the safe wire
		// representation of the rejected threshold.
		reportedThreshold = 0
	}
	report := Report{
		SchemaVersion:     ReportSchemaVersion,
		Status:            StatusInvalid,
		PassThreshold:     reportedThreshold,
		CriticalPassed:    true,
		Fingerprint:       plan.Fingerprint,
		FingerprintSHA256: plan.Fingerprint.Digest(),
		Checks:            make([]CheckResult, 0, len(plan.Checks)),
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	planInvalid := false
	if len(plan.Checks) == 0 {
		report.Errors = append(report.Errors, "plan must contain at least one check")
		planInvalid = true
	}
	if len(plan.Checks) > MaxChecksPerPlanV1 {
		report.Errors = append(report.Errors, "plan exceeds the v1 check limit")
		planInvalid = true
	}
	if !thresholdValid {
		report.Errors = append(report.Errors, "pass threshold must be a finite number in (0, 1]")
		planInvalid = true
	}

	seen := make(map[string]struct{}, len(plan.Checks))
	exactTotal := new(big.Rat)
	exactPassed := new(big.Rat)
	for _, check := range plan.Checks {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result, err := gradeCheckContext(ctx, check, evidence)
		if err != nil {
			return report, err
		}
		id := strings.TrimSpace(check.ID)
		if id == "" {
			result.Status = StatusInvalid
			result.Detail = "check id is required"
		} else if utf8.RuneCountInString(id) > MaxCheckIDRunesV1 {
			result.Status = StatusInvalid
			result.Detail = "check id exceeds the v1 length limit"
		} else if _, exists := seen[id]; exists {
			result.Status = StatusInvalid
			result.Detail = "duplicate check id"
		}
		seen[id] = struct{}{}

		nextTotal := report.WeightedTotal + validWeight(check.Weight)
		if math.IsNaN(nextTotal) || math.IsInf(nextTotal, 0) {
			result.Status = StatusInvalid
			result.Detail = "cumulative check weight exceeds the finite range"
			report.InvalidChecks++
			planInvalid = true
			if check.Critical {
				report.CriticalPassed = false
			}
			report.Checks = append(report.Checks, result)
			continue
		}
		report.WeightedTotal = nextTotal
		exactWeight := new(big.Rat)
		if isValidWeight(check.Weight) {
			exactWeight.SetFloat64(check.Weight)
		}
		exactTotal.Add(exactTotal, exactWeight)
		switch result.Status {
		case StatusPass:
			nextPassed := report.WeightedPassed + check.Weight
			if math.IsNaN(nextPassed) || math.IsInf(nextPassed, 0) {
				result.Status = StatusInvalid
				result.Detail = "cumulative passed weight exceeds the finite range"
				report.InvalidChecks++
				planInvalid = true
				if check.Critical {
					report.CriticalPassed = false
				}
				break
			}
			report.PassedChecks++
			report.WeightedPassed = nextPassed
			exactPassed.Add(exactPassed, exactWeight)
		case StatusFail:
			report.FailedChecks++
			if check.Critical {
				report.CriticalPassed = false
			}
		default:
			report.InvalidChecks++
			planInvalid = true
			if check.Critical {
				report.CriticalPassed = false
			}
		}
		report.Checks = append(report.Checks, result)
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	meetsThreshold := false
	if exactTotal.Sign() > 0 && !math.IsNaN(report.WeightedPassed) && !math.IsInf(report.WeightedPassed, 0) {
		exactScore := new(big.Rat).Quo(exactPassed, exactTotal)
		report.Score, _ = exactScore.Float64()
		if thresholdValid {
			thresholdRat := new(big.Rat).SetFloat64(threshold)
			meetsThreshold = exactScore.Cmp(thresholdRat) >= 0
		}
	}
	if math.IsNaN(report.Score) || math.IsInf(report.Score, 0) {
		report.Score = 0
		report.Errors = append(report.Errors, "weighted score is not finite")
		planInvalid = true
	}
	if planInvalid || report.WeightedTotal == 0 {
		report.Status = StatusInvalid
		return report, nil
	}
	if !report.CriticalPassed || !meetsThreshold {
		report.Status = StatusFail
		return report, nil
	}
	report.Status = StatusPass
	return report, nil
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

func gradeArtifactContext(ctx context.Context, check Check, evidence Evidence) (Status, string, error) {
	if err := ctx.Err(); err != nil {
		return StatusInvalid, "artifact grading was canceled", err
	}
	if strings.TrimSpace(evidence.ArtifactRoot) == "" {
		return StatusInvalid, "artifact root is required", nil
	}
	rawPath := strings.TrimSpace(check.ArtifactPath)
	rel := filepath.Clean(rawPath)
	if rel == "." || rel == "" || rel != rawPath || strings.ContainsRune(rel, '\x00') || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return StatusInvalid, "artifact path must be a safe relative file path", nil
	}
	want := strings.ToLower(strings.TrimSpace(check.ExpectedSHA256))
	decoded, err := hex.DecodeString(want)
	if err != nil || len(decoded) != sha256.Size || len(want) != sha256.Size*2 {
		return StatusInvalid, "expected artifact sha256 must be a full hexadecimal digest", nil
	}

	rootAbs, err := filepath.Abs(evidence.ArtifactRoot)
	if err != nil {
		return StatusInvalid, "artifact root could not be resolved", nil
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StatusFail, "artifact root does not exist", nil
		}
		return StatusInvalid, "artifact root could not be inspected", nil
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return StatusInvalid, "artifact root is not a directory", nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return StatusInvalid, "artifact root could not be resolved", nil
	}

	resolvedTarget := resolvedRoot
	var targetInfo os.FileInfo
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return StatusInvalid, "artifact grading was canceled", err
		}
		resolvedTarget = filepath.Join(resolvedTarget, part)
		info, statErr := os.Lstat(resolvedTarget)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return StatusFail, "artifact does not exist", nil
			}
			return StatusInvalid, "artifact could not be inspected", nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return StatusInvalid, "artifact path contains a symlink", nil
		}
		if i < len(parts)-1 && !info.IsDir() {
			return StatusFail, "artifact path parent is not a directory", nil
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return StatusFail, "artifact is not a regular file", nil
		}
		if i == len(parts)-1 {
			targetInfo = info
		}
	}
	maxBytes := int64(domainbriefcase.MaxArtifactBytesV1)
	if signed, ok := evidence.ArtifactMaxBytes[filepath.ToSlash(rel)]; ok && signed > 0 {
		maxBytes = signed
	}
	if targetInfo == nil || targetInfo.Size() > maxBytes {
		return StatusInvalid, "artifact exceeds its signed size limit", nil
	}

	f, err := os.Open(resolvedTarget)
	if err != nil {
		return StatusInvalid, "artifact could not be opened", nil
	}
	defer f.Close()
	h := sha256.New()
	limited := &io.LimitedReader{R: f, N: maxBytes + 1}
	if err := hashReaderContext(ctx, h, limited); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return StatusInvalid, "artifact grading was canceled", err
		}
		return StatusInvalid, "artifact could not be hashed", nil
	}
	if limited.N <= 0 {
		return StatusInvalid, "artifact exceeds its signed size limit", nil
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return StatusFail, "artifact sha256 did not match", nil
	}
	return StatusPass, "artifact exists and sha256 matched", nil
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
