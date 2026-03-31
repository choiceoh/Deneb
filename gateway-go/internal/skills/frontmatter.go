package skills

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// ParsedFrontmatter is a map of key-value pairs extracted from a SKILL.md header.
type ParsedFrontmatter map[string]string

// DenebSkillMetadata represents parsed skill metadata from frontmatter.
type DenebSkillMetadata struct {
	Always     bool               `json:"always,omitempty"`
	SkillKey   string             `json:"skillKey,omitempty"`
	PrimaryEnv string             `json:"primaryEnv,omitempty"`
	Emoji      string             `json:"emoji,omitempty"`
	Homepage   string             `json:"homepage,omitempty"`
	Tags       []string           `json:"tags,omitempty"`
	Requires   *SkillRequires     `json:"requires,omitempty"`
	Install    []SkillInstallSpec `json:"install,omitempty"`
}

// SkillRequires defines dependency requirements for a skill.
type SkillRequires struct {
	Bins    []string `json:"bins,omitempty"`
	AnyBins []string `json:"anyBins,omitempty"`
	Env     []string `json:"env,omitempty"`
	Config  []string `json:"config,omitempty"`
}

// SkillInstallSpec represents an installation specification for a skill dependency.
type SkillInstallSpec struct {
	ID              string   `json:"id,omitempty"`
	Kind            string   `json:"kind"`
	Label           string   `json:"label,omitempty"`
	Bins            []string `json:"bins,omitempty"`
	Formula         string   `json:"formula,omitempty"`
	Package         string   `json:"package,omitempty"`
	Module          string   `json:"module,omitempty"`
	URL             string   `json:"url,omitempty"`
	Archive         string   `json:"archive,omitempty"`
	Extract         *bool    `json:"extract,omitempty"`
	StripComponents *int     `json:"stripComponents,omitempty"`
	TargetDir       string   `json:"targetDir,omitempty"`
}

// SkillInvocationPolicy controls how a skill can be invoked.
type SkillInvocationPolicy struct {
	UserInvocable          bool `json:"userInvocable"`
	DisableModelInvocation bool `json:"disableModelInvocation"`
}

// Validation patterns for install spec fields.
var (
	brewFormulaPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+._/-]*$`)
	goModulePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+\-/]*(?:@[A-Za-z0-9][A-Za-z0-9._~+\-/]*)?$`)
	uvPackagePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-\[\]=<>!~+,]*$`)
)

// Allowed install spec kinds.
var allowedInstallKinds = map[string]bool{
	"brew": true, "node": true, "go": true, "uv": true, "download": true,
}

// ExtractFrontmatterBlock returns only the frontmatter portion of content
// (between the first two "---" delimiters), enabling progressive loading
// where Stage 1 reads only the header for metadata, not the full body.
// Returns the raw frontmatter block (including delimiters) and the byte
// offset where the body begins. If no valid frontmatter is found, returns
// empty string and 0.
func ExtractFrontmatterBlock(content string) (header string, bodyOffset int) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return "", 0
	}

	// Find opening delimiter.
	startIdx := -1
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			startIdx = i
			break
		}
		if trimmed != "" {
			return "", 0
		}
		offset += len(line) + 1 // +1 for newline
	}
	if startIdx < 0 {
		return "", 0
	}

	// Find closing delimiter.
	headerEnd := offset + len(lines[startIdx]) + 1
	for i := startIdx + 1; i < len(lines); i++ {
		headerEnd += len(lines[i]) + 1
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			return content[:headerEnd], headerEnd
		}
	}

	// No closing delimiter found — treat entire content as frontmatter.
	return content, len(content)
}

// ParseFrontmatter extracts frontmatter key-value pairs from a SKILL.md content.
// Supports YAML-style frontmatter delimited by "---".
func ParseFrontmatter(content string) ParsedFrontmatter {
	result := make(ParsedFrontmatter)
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return result
	}

	// Find opening delimiter.
	startIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			startIdx = i
			break
		}
		if trimmed != "" {
			return result // Non-empty line before delimiter.
		}
	}
	if startIdx < 0 {
		return result
	}

	// Find closing delimiter and extract key-value pairs.
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			break
		}
		// Parse key: value pairs.
		colonIdx := strings.IndexByte(trimmed, ':')
		if colonIdx < 1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		value := strings.TrimSpace(trimmed[colonIdx+1:])
		// Strip surrounding quotes.
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		result[key] = value
	}
	return result
}

// ResolveDenebMetadata parses and validates skill metadata from frontmatter.
// Returns nil if no metadata block is found.
func ResolveDenebMetadata(frontmatter ParsedFrontmatter) *DenebSkillMetadata {
	raw, ok := frontmatter["metadata"]
	if !ok || raw == "" {
		return nil
	}

	// Parse the metadata JSON block.
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil
	}

	denebRaw, ok := outer["deneb"]
	if !ok {
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(denebRaw, &obj); err != nil {
		return nil
	}

	meta := &DenebSkillMetadata{}

	// Parse simple fields.
	parseJSONBool(obj, "always", &meta.Always)
	parseJSONString(obj, "skillKey", &meta.SkillKey)
	parseJSONString(obj, "primaryEnv", &meta.PrimaryEnv)
	parseJSONString(obj, "emoji", &meta.Emoji)
	parseJSONString(obj, "homepage", &meta.Homepage)
	// Parse tags.
	meta.Tags = parseJSONStringList(obj, "tags")

	// Parse requires.
	if reqRaw, ok := obj["requires"]; ok {
		var reqObj map[string]json.RawMessage
		if json.Unmarshal(reqRaw, &reqObj) == nil {
			meta.Requires = &SkillRequires{
				Bins:    parseJSONStringList(reqObj, "bins"),
				AnyBins: parseJSONStringList(reqObj, "anyBins"),
				Env:     parseJSONStringList(reqObj, "env"),
				Config:  parseJSONStringList(reqObj, "config"),
			}
		}
	}

	// Parse install specs.
	if installRaw, ok := obj["install"]; ok {
		var installArr []json.RawMessage
		if json.Unmarshal(installRaw, &installArr) == nil {
			for _, specRaw := range installArr {
				if spec := parseInstallSpec(specRaw); spec != nil {
					meta.Install = append(meta.Install, *spec)
				}
			}
		}
	}

	return meta
}

// ResolveSkillInvocationPolicy extracts invocation policy from frontmatter.
func ResolveSkillInvocationPolicy(frontmatter ParsedFrontmatter) SkillInvocationPolicy {
	return SkillInvocationPolicy{
		UserInvocable:          parseFrontmatterBool(frontmatter, "user-invocable", true),
		DisableModelInvocation: parseFrontmatterBool(frontmatter, "disable-model-invocation", false),
	}
}

// parseInstallSpec parses and validates a single install specification.
func parseInstallSpec(raw json.RawMessage) *SkillInstallSpec {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}

	var kind string
	parseJSONString(obj, "kind", &kind)
	if !allowedInstallKinds[kind] {
		return nil
	}

	spec := &SkillInstallSpec{Kind: kind}
	parseJSONString(obj, "id", &spec.ID)
	parseJSONString(obj, "label", &spec.Label)
	spec.Bins = parseJSONStringList(obj, "bins")

	switch kind {
	case "brew":
		parseJSONString(obj, "formula", &spec.Formula)
		spec.Formula = normalizeSafeBrewFormula(spec.Formula)
		if spec.Formula == "" {
			return nil
		}
	case "node":
		parseJSONString(obj, "package", &spec.Package)
		if spec.Package == "" {
			return nil
		}
	case "go":
		parseJSONString(obj, "module", &spec.Module)
		spec.Module = normalizeSafeGoModule(spec.Module)
		if spec.Module == "" {
			return nil
		}
	case "uv":
		parseJSONString(obj, "package", &spec.Package)
		spec.Package = normalizeSafeUvPackage(spec.Package)
		if spec.Package == "" {
			return nil
		}
	case "download":
		parseJSONString(obj, "url", &spec.URL)
		spec.URL = normalizeSafeDownloadURL(spec.URL)
		if spec.URL == "" {
			return nil
		}
		parseJSONString(obj, "archive", &spec.Archive)
		parseJSONString(obj, "targetDir", &spec.TargetDir)

		var extract bool
		if parseJSONBool(obj, "extract", &extract) {
			spec.Extract = &extract
		}
		var strip float64
		if parseJSONFloat(obj, "stripComponents", &strip) {
			v := int(strip)
			spec.StripComponents = &v
		}
	}

	return spec
}

// --- Validation helpers ---

func normalizeSafeBrewFormula(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s[0] == '-' || strings.Contains(s, "\\") || strings.Contains(s, "..") {
		return ""
	}
	if !brewFormulaPattern.MatchString(s) {
		return ""
	}
	return s
}

func normalizeSafeGoModule(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s[0] == '-' || strings.Contains(s, "\\") || strings.Contains(s, "://") {
		return ""
	}
	if !goModulePattern.MatchString(s) {
		return ""
	}
	return s
}

func normalizeSafeUvPackage(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s[0] == '-' || strings.Contains(s, "\\") || strings.Contains(s, "://") {
		return ""
	}
	if !uvPackagePattern.MatchString(s) {
		return ""
	}
	return s
}

func normalizeSafeDownloadURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, " \t\n\r") {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return s
}

// --- JSON parsing helpers ---

func parseJSONString(obj map[string]json.RawMessage, key string, out *string) bool {
	raw, ok := obj[key]
	if !ok {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		*out = s
		return true
	}
	return false
}

func parseJSONBool(obj map[string]json.RawMessage, key string, out *bool) bool {
	raw, ok := obj[key]
	if !ok {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		*out = b
		return true
	}
	return false
}

func parseJSONFloat(obj map[string]json.RawMessage, key string, out *float64) bool {
	raw, ok := obj[key]
	if !ok {
		return false
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		*out = f
		return true
	}
	return false
}

func parseJSONStringList(obj map[string]json.RawMessage, key string) []string {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	return nil
}

func parseFrontmatterBool(fm ParsedFrontmatter, key string, fallback bool) bool {
	v, ok := fm[key]
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	default:
		return fallback
	}
}
