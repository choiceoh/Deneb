package briefcase

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	toolPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

var briefcaseV1Tools = map[string]struct{}{
	"mail_archive": {}, "contacts": {}, "files": {}, "calendar": {}, "todo": {},
	"phone_read": {}, "phone_write": {}, "wiki": {}, "knowledge": {}, "polaris": {}, "notebook": {},
	"read": {}, "grep": {}, "write": {}, "edit": {},
}

const (
	maxSourceRefBytes   = 256
	maxProjectRefs      = 4
	maxSupersedes       = 4
	maxSensitivityBytes = 64
)

// ValidationError contains all independently detectable contract violations in
// deterministic order. A casepack must have zero problems before it is usable.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "briefcase: invalid casepack: " + strings.Join(e.Problems, "; ")
}

type validator struct {
	pack       *Pack
	problems   []string
	referenced map[string]string
	sources    map[string]Source
	artifacts  map[string]Artifact
	outputs    map[string]string
	released   map[string]string
}

// Validate authenticates a loaded directory pack and enforces all v1
// visibility, identity, path, and policy invariants.
func Validate(pack *Pack) error {
	if pack == nil {
		return &ValidationError{Problems: []string{"pack is nil"}}
	}
	v := &validator{
		pack:       pack,
		referenced: map[string]string{ManifestFile: "manifest"},
		sources:    make(map[string]Source),
		artifacts:  make(map[string]Artifact),
		outputs:    make(map[string]string),
		released:   make(map[string]string),
	}
	v.validateManifest()
	v.validateSources()
	v.validateArtifacts()
	v.validateEpisodes()
	v.validateRunPolicy()
	v.validateToolPolicy()
	v.validateNetworkPolicy()
	v.validateSupersessionGraph()
	v.validateFiles()

	if len(v.problems) == 0 {
		pack.Digest = pack.Manifest.ManifestDigest
		pack.hashes = declaredHashes(pack.Manifest)
		return nil
	}
	sort.Strings(v.problems)
	return &ValidationError{Problems: v.problems}
}

func (v *validator) add(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

func (v *validator) validateManifest() {
	m := v.pack.Manifest
	if m.SchemaVersion != SchemaVersionV1 {
		v.add("schemaVersion %q is unsupported (want %q)", m.SchemaVersion, SchemaVersionV1)
	}
	if !validID(m.CaseID) {
		v.add("caseId %q is invalid", m.CaseID)
	}
	if !validID(m.FamilyID) {
		v.add("familyId %q is invalid", m.FamilyID)
	}
	if !oneOf(string(m.Split), string(SplitDev), string(SplitCalibration), string(SplitHoldout)) {
		v.add("split %q is invalid", m.Split)
	}
	if !oneOf(string(m.PrivacyMode), string(PrivacyPortable), string(PrivacyVault)) {
		v.add("privacyMode %q is invalid", m.PrivacyMode)
	}
	if m.Seed <= 0 {
		v.add("seed must be positive; zero is reserved as unset")
	}
	if m.CutoffAt.IsZero() {
		v.add("cutoffAt is required")
	}
	if m.FrozenNow.IsZero() {
		v.add("frozenNow is required")
	}
	if !m.CutoffAt.IsZero() && !m.FrozenNow.IsZero() && m.FrozenNow.Before(m.CutoffAt) {
		v.add("frozenNow %s is before cutoffAt %s", stamp(m.FrozenNow), stamp(m.CutoffAt))
	}
	if strings.TrimSpace(m.Timezone) == "" {
		v.add("timezone is required")
	} else if _, err := time.LoadLocation(m.Timezone); err != nil {
		v.add("timezone %q is invalid", m.Timezone)
	}
	if m.Locale != "ko-KR" {
		v.add("locale %q is unsupported in schema v1 (want ko-KR)", m.Locale)
	}
	if !sha256Pattern.MatchString(m.ManifestDigest) {
		v.add("manifestDigest must be lowercase SHA-256")
	} else if digest, err := CanonicalDigest(m); err != nil {
		v.add("canonical manifest digest failed: %v", err)
	} else if digest != m.ManifestDigest {
		v.add("manifestDigest mismatch: got %s, want %s", m.ManifestDigest, digest)
	}
}

func (v *validator) validateRunPolicy() {
	p := v.pack.Manifest.RunPolicy
	if p.MaxTurns <= 0 {
		v.add("runPolicy.maxTurns must be positive")
	} else if p.MaxTurns > MaxTurnsV1 {
		v.add("runPolicy.maxTurns %d exceeds schema v1 hard cap %d", p.MaxTurns, MaxTurnsV1)
	}
	if p.TimeoutSeconds <= 0 {
		v.add("runPolicy.timeoutSeconds must be positive")
	} else if p.TimeoutSeconds > MaxTimeoutSecondsV1 {
		v.add("runPolicy.timeoutSeconds exceeds schema v1 hard cap %d", MaxTimeoutSecondsV1)
	}
	if p.MaxTokens <= 0 {
		v.add("runPolicy.maxTokens must be positive")
	} else if p.MaxTokens > MaxTokensV1 {
		v.add("runPolicy.maxTokens exceeds schema v1 hard cap %d", MaxTokensV1)
	}
	if p.MaxFollowUps < 0 || p.MaxFollowUps > MaxFollowUpsV1 {
		v.add("runPolicy.maxFollowUps must be between 0 and %d", MaxFollowUpsV1)
	}
	if p.PerTurnTimeoutSeconds < 0 {
		v.add("runPolicy.perTurnTimeoutSeconds must not be negative")
	} else if p.PerTurnTimeoutSeconds > MaxTimeoutSecondsV1 {
		v.add("runPolicy.perTurnTimeoutSeconds exceeds schema v1 hard cap %d", MaxTimeoutSecondsV1)
	} else if p.PerTurnTimeoutSeconds > 0 && p.PerTurnTimeoutSeconds > p.TimeoutSeconds {
		v.add("runPolicy.perTurnTimeoutSeconds must not exceed timeoutSeconds")
	}
}

func (v *validator) validateSources() {
	m := v.pack.Manifest
	if len(m.Sources) > MaxSourcesV1 {
		v.add("sources exceed schema v1 hard cap %d", MaxSourcesV1)
	}
	for i, source := range m.Sources {
		label := fmt.Sprintf("sources[%d]", i)
		if !validID(source.ID) {
			v.add("%s.id %q is invalid", label, source.ID)
		} else if _, exists := v.sources[source.ID]; exists {
			v.add("duplicate source id %q", source.ID)
		} else {
			v.sources[source.ID] = source
		}
		if !validSourceKind(source.Kind) {
			v.add("source %q kind %q is invalid", source.ID, source.Kind)
		}
		if !oneOf(string(source.Origin), string(SourceOriginExternal), string(SourceOriginHuman), string(SourceOriginDenebGenerated), string(SourceOriginSynthetic)) {
			v.add("source %q origin %q is invalid", source.ID, source.Origin)
		}
		if !oneOf(string(source.Access), string(SourceAccessSnapshot), string(SourceAccessTimeline), string(SourceAccessSealed)) {
			v.add("source %q access %q is invalid", source.ID, source.Access)
		}
		v.validateAssetPath(label, source.Path, source.SHA256, sourcePrefix(source.Access))
		v.validateRecallSource(source)
		if source.EventAt.IsZero() {
			v.add("source %q eventAt is required", source.ID)
		}
		if source.AvailableAt.IsZero() {
			v.add("source %q availableAt is required", source.ID)
		}
		if source.CapturedAt.IsZero() {
			v.add("source %q capturedAt is required", source.ID)
		}
		if !source.AvailableAt.IsZero() && !source.CapturedAt.IsZero() && source.CapturedAt.Before(source.AvailableAt) {
			v.add("source %q capturedAt is before availableAt", source.ID)
		}
		if source.Access == SourceAccessSnapshot && !source.AvailableAt.IsZero() && source.AvailableAt.After(m.CutoffAt) {
			v.add("source %q is future data exposed in snapshot: availableAt %s is after cutoffAt %s", source.ID, stamp(source.AvailableAt), stamp(m.CutoffAt))
		}
		if source.Memory && source.Access == SourceAccessSealed {
			v.add("source %q cannot mark sealed grader evidence as memory", source.ID)
		}
		if len(source.SourceRef) > maxSourceRefBytes {
			v.add("source %q sourceRef exceeds %d bytes", source.ID, maxSourceRefBytes)
		}
		if len(source.ProjectRefs) > maxProjectRefs {
			v.add("source %q projectRefs exceeds %d entries", source.ID, maxProjectRefs)
		}
		if len(source.Supersedes) > maxSupersedes {
			v.add("source %q supersedes exceeds %d entries", source.ID, maxSupersedes)
		}
		if len(source.Sensitivity) > maxSensitivityBytes {
			v.add("source %q sensitivity exceeds %d bytes", source.ID, maxSensitivityBytes)
		}
		validateUniqueStrings(v, "source "+source.ID+" projectRefs", source.ProjectRefs, true)
		validateUniqueStrings(v, "source "+source.ID+" supersedes", source.Supersedes, true)
	}
}

func (v *validator) validateRecallSource(source Source) {
	textKind := source.Kind == SourceWiki || source.Kind == SourceDiary || source.Kind == SourceNotebook ||
		source.Kind == SourceTranscript || source.Kind == SourceWorkfeed
	if source.Access == SourceAccessSealed || (!source.Memory && !textKind) || v.pack.Root == "" || validateRelativePath(source.Path) != nil {
		return
	}
	data, err := readSecureFile(v.pack.Root, source.Path, MaxRecallSourceBytesV1)
	if err != nil {
		v.add("source %q recall text: %v", source.ID, err)
		return
	}
	if !utf8.Valid(data) {
		v.add("source %q recall text must be valid UTF-8", source.ID)
		return
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			v.add("source %q recall text contains forbidden control character", source.ID)
			return
		}
	}
}

func (v *validator) validateArtifacts() {
	if len(v.pack.Manifest.Artifacts) > MaxArtifactsV1 {
		v.add("artifacts exceed schema v1 hard cap %d", MaxArtifactsV1)
	}
	for i, artifact := range v.pack.Manifest.Artifacts {
		label := fmt.Sprintf("artifacts[%d]", i)
		if !validID(artifact.ID) {
			v.add("%s.id %q is invalid", label, artifact.ID)
		} else if _, exists := v.artifacts[artifact.ID]; exists {
			v.add("duplicate artifact id %q", artifact.ID)
		} else {
			v.artifacts[artifact.ID] = artifact
		}
		if err := validateRelativePath(artifact.Path); err != nil {
			v.add("artifact %q path: %v", artifact.ID, err)
		} else if !hasDirPrefix(artifact.Path, "output") {
			v.add("artifact %q path %q must be under output/", artifact.ID, artifact.Path)
		} else if prior, exists := v.outputs[artifact.Path]; exists {
			v.add("artifact output path %q is shared by %q and %q", artifact.Path, prior, artifact.ID)
		} else {
			v.outputs[artifact.Path] = artifact.ID
		}
		if strings.TrimSpace(artifact.MIME) == "" || !strings.Contains(artifact.MIME, "/") {
			v.add("artifact %q mime %q is invalid", artifact.ID, artifact.MIME)
		}
		if artifact.MaxBytes < 0 {
			v.add("artifact %q maxBytes must not be negative", artifact.ID)
		} else if artifact.MaxBytes > MaxArtifactBytesV1 {
			v.add("artifact %q maxBytes exceeds schema v1 hard cap %d", artifact.ID, MaxArtifactBytesV1)
		}
	}
}

func (v *validator) validateEpisodes() {
	m := v.pack.Manifest
	if len(m.Episodes) == 0 {
		v.add("episodes must contain at least one event")
	} else if len(m.Episodes) > MaxEpisodesV1 {
		v.add("episodes exceed schema v1 hard cap %d", MaxEpisodesV1)
	}
	seen := make(map[string]struct{})
	var previous time.Time
	for i, episode := range m.Episodes {
		label := fmt.Sprintf("episodes[%d]", i)
		if !validID(episode.ID) {
			v.add("%s.id %q is invalid", label, episode.ID)
		} else if _, exists := seen[episode.ID]; exists {
			v.add("duplicate episode id %q", episode.ID)
		} else {
			seen[episode.ID] = struct{}{}
		}
		if !oneOf(string(episode.Kind), string(EpisodeUserTurn), string(EpisodeEvent), string(EpisodeHeartbeat)) {
			v.add("episode %q kind %q is invalid", episode.ID, episode.Kind)
		}
		if episode.At.IsZero() {
			v.add("episode %q at is required", episode.ID)
		} else {
			if episode.At.Before(m.FrozenNow) {
				v.add("episode %q at %s is before frozenNow %s", episode.ID, stamp(episode.At), stamp(m.FrozenNow))
			}
			if !previous.IsZero() && episode.At.Before(previous) {
				v.add("episode %q is out of chronological order", episode.ID)
			}
			previous = episode.At
		}
		if (episode.Kind == EpisodeUserTurn || episode.Kind == EpisodeHeartbeat) && episode.Input == nil {
			v.add("executable episode %q requires input", episode.ID)
		}
		if episode.Input != nil {
			v.validateAssetPath(label+".input", episode.Input.Path, episode.Input.SHA256, "timeline")
			v.validateEpisodeInput(episode.ID, episode.Input.Path)
		}
		for _, sourceID := range episode.ReleaseSourceIDs {
			source, exists := v.sources[sourceID]
			if !exists {
				v.add("episode %q releases unknown source %q", episode.ID, sourceID)
				continue
			}
			if prior, exists := v.released[sourceID]; exists {
				v.add("source %q is released more than once by episodes %q and %q", sourceID, prior, episode.ID)
			} else {
				v.released[sourceID] = episode.ID
			}
			switch source.Access {
			case SourceAccessSealed:
				v.add("episode %q exposes sealed source %q", episode.ID, sourceID)
			case SourceAccessSnapshot:
				v.add("episode %q re-releases snapshot source %q", episode.ID, sourceID)
			case SourceAccessTimeline:
				if !episode.At.IsZero() && !source.AvailableAt.IsZero() && source.AvailableAt.After(episode.At) {
					v.add("episode %q exposes future source %q at %s before availableAt %s", episode.ID, sourceID, stamp(episode.At), stamp(source.AvailableAt))
				}
			}
		}
		validateUniqueStrings(v, "episode "+episode.ID+" releaseSourceIds", episode.ReleaseSourceIDs, true)
		validateUniqueStrings(v, "episode "+episode.ID+" expectedArtifactIds", episode.ExpectedArtifactIDs, true)
		for _, artifactID := range episode.ExpectedArtifactIDs {
			if _, exists := v.artifacts[artifactID]; !exists {
				v.add("episode %q expects unknown artifact %q", episode.ID, artifactID)
			}
		}
	}
	for _, source := range m.Sources {
		if source.Access == SourceAccessTimeline {
			if _, exists := v.released[source.ID]; !exists {
				v.add("timeline source %q is never released", source.ID)
			}
		}
	}
}

func (v *validator) validateEpisodeInput(episodeID, relative string) {
	if v.pack.Root == "" || validateRelativePath(relative) != nil {
		return
	}
	data, err := readSecureFile(v.pack.Root, relative, MaxEpisodeInputBytesV1)
	if err != nil {
		v.add("episode %q input: %v", episodeID, err)
		return
	}
	if !utf8.Valid(data) {
		v.add("episode %q input must be valid UTF-8", episodeID)
		return
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			v.add("episode %q input contains forbidden control character", episodeID)
			return
		}
	}
}

func (v *validator) validateToolPolicy() {
	p := v.pack.Manifest.ToolPolicy
	if p.Default != ToolDeny {
		v.add("toolPolicy.default must be %q in schema v1", ToolDeny)
	}
	if p.MaxCalls <= 0 {
		v.add("toolPolicy.maxCalls must be positive")
	} else if p.MaxCalls > MaxToolCallsV1 {
		v.add("toolPolicy.maxCalls exceeds schema v1 hard cap %d", MaxToolCallsV1)
	}
	if len(p.Rules) > MaxToolRulesV1 {
		v.add("toolPolicy.rules exceed schema v1 hard cap %d", MaxToolRulesV1)
	}
	seen := make(map[string]struct{})
	for _, rule := range p.Rules {
		if !toolPattern.MatchString(rule.Name) {
			v.add("tool rule name %q is invalid", rule.Name)
		}
		if _, exists := seen[rule.Name]; exists {
			v.add("duplicate tool rule %q", rule.Name)
		}
		seen[rule.Name] = struct{}{}
		if _, supported := briefcaseV1Tools[rule.Name]; !supported {
			v.add("tool %q is not implemented by the Briefcase v1 runtime", rule.Name)
		}
		if !oneOf(string(rule.Decision), string(ToolDeny), string(ToolAllow)) {
			v.add("tool %q decision %q is invalid", rule.Name, rule.Decision)
		}
		if rule.MaxCalls < 0 {
			v.add("tool %q maxCalls must not be negative", rule.Name)
		}
		if rule.MaxCalls > p.MaxCalls {
			v.add("tool %q maxCalls exceeds global maxCalls", rule.Name)
		}
	}
}

func (v *validator) validateNetworkPolicy() {
	p := v.pack.Manifest.NetworkPolicy
	if p.Mode != NetworkDeny {
		v.add("networkPolicy.mode must be %q in schema v1", NetworkDeny)
	}
	if len(p.AllowedHosts) != 0 {
		v.add("network deny policy must not contain allowedHosts")
	}
}

func (v *validator) validateSupersessionGraph() {
	for _, source := range v.pack.Manifest.Sources {
		for _, prior := range source.Supersedes {
			if prior == source.ID {
				v.add("source %q cannot supersede itself", source.ID)
			} else if _, exists := v.sources[prior]; !exists {
				v.add("source %q supersedes unknown source %q", source.ID, prior)
			}
		}
	}
	state := make(map[string]uint8)
	var visit func(string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		for _, next := range v.sources[id].Supersedes {
			if _, exists := v.sources[next]; exists && visit(next) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range v.sources {
		if state[id] == 0 && visit(id) {
			v.add("source supersession graph contains a cycle involving %q", id)
			break
		}
	}
}

func (v *validator) validateFiles() {
	if v.pack.Root == "" {
		v.add("pack root is required")
		return
	}
	if v.pack.files == nil {
		files, err := scanTree(v.pack.Root)
		if err != nil {
			v.add("scan pack: %v", err)
			return
		}
		v.pack.files = files
	}
	for file := range v.pack.files {
		if _, exists := v.referenced[file]; !exists {
			v.add("unreferenced file is forbidden: %q", file)
		}
	}
	for file, owner := range v.referenced {
		if file == ManifestFile {
			continue
		}
		if _, exists := v.pack.files[file]; !exists {
			v.add("%s references missing file %q", owner, file)
		}
	}
}

func (v *validator) validateAssetPath(owner, relative, expectedHash, prefix string) {
	if err := validateRelativePath(relative); err != nil {
		v.add("%s path: %v", owner, err)
		return
	}
	if prefix == "" || !hasDirPrefix(relative, prefix) {
		v.add("%s path %q must be under %s/", owner, relative, prefix)
	}
	if prior, exists := v.referenced[relative]; exists {
		v.add("file %q is referenced by both %s and %s", relative, prior, owner)
	} else {
		v.referenced[relative] = owner
	}
	if !sha256Pattern.MatchString(expectedHash) {
		v.add("%s sha256 must be lowercase SHA-256", owner)
		return
	}
	if v.pack.Root == "" {
		return
	}
	actual, err := digestSecureFile(v.pack.Root, relative)
	if err != nil {
		v.add("%s cannot hash %q: %v", owner, relative, err)
	} else if actual != expectedHash {
		v.add("%s hash mismatch for %q: got %s, want %s", owner, relative, actual, expectedHash)
	}
}

func digestSecureFile(root, relative string) (string, error) {
	full, err := secureJoin(root, relative, true)
	if err != nil {
		return "", err
	}
	return FileDigest(full)
}

func sourcePrefix(access SourceAccess) string {
	switch access {
	case SourceAccessSnapshot:
		return "snapshot"
	case SourceAccessTimeline:
		return "timeline"
	case SourceAccessSealed:
		return "sealed"
	default:
		return ""
	}
}

func hasDirPrefix(relative, prefix string) bool {
	return strings.HasPrefix(relative, prefix+"/") && len(relative) > len(prefix)+1
}

func validID(value string) bool { return idPattern.MatchString(value) }

func validSourceKind(kind SourceKind) bool {
	return oneOf(string(kind), string(SourceMail), string(SourceCalendar), string(SourceWiki), string(SourceDiary), string(SourceCapture), string(SourceFile), string(SourceNotebook), string(SourceWorkfeed), string(SourceTranscript), string(SourceDevice))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateUniqueStrings(v *validator, owner string, values []string, requireID bool) {
	seen := make(map[string]struct{})
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			v.add("%s contains a blank value", owner)
		}
		if requireID && !validID(value) {
			v.add("%s contains invalid id %q", owner, value)
		}
		if _, exists := seen[value]; exists {
			v.add("%s contains duplicate %q", owner, value)
		}
		seen[value] = struct{}{}
	}
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
