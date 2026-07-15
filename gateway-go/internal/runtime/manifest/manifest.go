// Package manifest builds a content-addressed, redacted description of the
// composition that the running gateway is actually using.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

const (
	// SchemaVersion is bumped when the public manifest shape or digest inputs
	// change incompatibly.
	SchemaVersion = 1

	StateLoaded      = "loaded"
	StatePending     = "pending"
	StateDegraded    = "degraded"
	StateUnavailable = "unavailable"

	// Keep health polling bounded by the same per-file ceiling used by skill
	// discovery. A corrupted/replaced SKILL.md must not turn /health into an
	// unbounded file reader.
	maxSkillFileBytes int64 = 256_000
)

// Component is the redacted public identity of one runtime component. Names,
// paths, endpoints, schemas, and credentials are never exposed by the health
// endpoint; only their count and content digest leave this package.
type Component struct {
	State  string `json:"state"`
	Count  int    `json:"count"`
	SHA256 string `json:"sha256,omitempty"`
}

// BinaryComponent identifies the executable bytes and the build version that
// launched the process.
type BinaryComponent struct {
	State   string `json:"state"`
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
}

// Snapshot is the safe subset published under /health.runtime_manifest.
type Snapshot struct {
	SchemaVersion int             `json:"schema_version"`
	SHA256        string          `json:"sha256"`
	Binary        BinaryComponent `json:"binary"`
	Skills        Component       `json:"skills"`
	Tools         Component       `json:"tools"`
	Models        Component       `json:"models"`
}

// Tool is the non-secret identity of one registered tool. Function bytes are
// covered by the executable digest; the remaining fields capture the runtime
// registration, including asynchronously discovered external MCP schemas.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Schema      string `json:"schema"`
	SchemaValid bool   `json:"schema_valid"`
	Hidden      bool   `json:"hidden"`
	Deferred    bool   `json:"deferred"`
	Profile     string `json:"profile"`
	MaxOutput   int    `json:"max_output"`
}

// Model is the effective role mapping. CredentialSet deliberately records only
// presence: credentials themselves never enter a digest exposed over HTTP.
type Model struct {
	Role          string `json:"role"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	APIMode       string `json:"api_mode"`
	CredentialSet bool   `json:"credential_set"`
}

// Skill points to the exact instruction file used by the discovered skill.
// Path is used only for reading and is excluded from every public digest so
// equivalent deployments at different filesystem roots remain comparable.
type Skill struct {
	Name    string
	Version string
	Path    string
}

// Input is a point-in-time view of the registries owned by the gateway.
type Input struct {
	Version      string
	ToolsLoaded  bool
	Tools        []Tool
	ModelsLoaded bool
	Models       []Model
	SkillsLoaded bool
	Skills       []Skill
}

type cachedFileDigest struct {
	size       int64
	modifiedNs int64
	sha256     string
}

// Builder owns the executable identity and a metadata-keyed cache of skill
// file hashes. Health polling stats skill files on every request but rereads
// their contents only when size or modification time changes.
type Builder struct {
	binaryState  string
	binarySHA256 string

	mu        sync.Mutex
	fileCache map[string]cachedFileDigest
}

var executableIdentity struct {
	once   sync.Once
	state  string
	sha256 string
}

var errFileTooLarge = errors.New("manifest file exceeds size limit")

// NewBuilder captures the current executable bytes once per process.
func NewBuilder() *Builder {
	executableIdentity.once.Do(func() {
		executableIdentity.state = StateUnavailable
		path, err := os.Executable()
		if err != nil {
			return
		}
		executableIdentity.sha256, err = digestFile(path)
		if err == nil {
			executableIdentity.state = StateLoaded
		}
	})
	return newBuilder(executableIdentity.state, executableIdentity.sha256)
}

func newBuilder(binaryState, binarySHA256 string) *Builder {
	return &Builder{
		binaryState:  binaryState,
		binarySHA256: binarySHA256,
		fileCache:    make(map[string]cachedFileDigest),
	}
}

// Build returns a deterministic snapshot. Slice order and host filesystem roots
// do not affect its digest.
func (b *Builder) Build(input Input) Snapshot {
	tools := component(input.ToolsLoaded, toolIdentities(input.Tools))
	if tools.State == StateLoaded {
		for _, tool := range input.Tools {
			if !tool.SchemaValid {
				tools.State = StateDegraded
				break
			}
		}
	}
	models := component(input.ModelsLoaded, modelIdentities(input.Models))
	skills := b.skillsComponent(input.SkillsLoaded, input.Skills)

	snapshot := Snapshot{
		SchemaVersion: SchemaVersion,
		Binary: BinaryComponent{
			State:   b.binaryState,
			Version: input.Version,
			SHA256:  b.binarySHA256,
		},
		Skills: skills,
		Tools:  tools,
		Models: models,
	}
	snapshot.SHA256 = digestJSON(snapshot)
	return snapshot
}

func component(loaded bool, identities []string) Component {
	if !loaded {
		return Component{State: StatePending}
	}
	sort.Strings(identities)
	return Component{
		State:  StateLoaded,
		Count:  len(identities),
		SHA256: digestJSON(identities),
	}
}

func toolIdentities(tools []Tool) []string {
	identities := make([]string, 0, len(tools))
	for _, tool := range tools {
		identities = append(identities, digestJSON(tool))
	}
	return identities
}

func modelIdentities(models []Model) []string {
	identities := make([]string, 0, len(models))
	for _, model := range models {
		identities = append(identities, digestJSON(model))
	}
	return identities
}

type skillIdentity struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	ContentSHA256 string `json:"content_sha256"`
}

func (b *Builder) skillsComponent(loaded bool, skills []Skill) Component {
	if !loaded {
		return Component{State: StatePending}
	}
	state := StateLoaded
	identities := make([]string, 0, len(skills))
	for _, skill := range skills {
		contentSHA256, ok := b.skillFileDigest(skill.Path)
		if !ok {
			state = StateDegraded
		}
		identities = append(identities, digestJSON(skillIdentity{
			Name:          skill.Name,
			Version:       skill.Version,
			ContentSHA256: contentSHA256,
		}))
	}
	sort.Strings(identities)
	return Component{
		State:  state,
		Count:  len(identities),
		SHA256: digestJSON(identities),
	}
}

func (b *Builder) skillFileDigest(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxSkillFileBytes {
		return StateUnavailable, false
	}

	b.mu.Lock()
	cached, ok := b.fileCache[path]
	b.mu.Unlock()
	if ok && cached.size == info.Size() && cached.modifiedNs == info.ModTime().UnixNano() {
		return cached.sha256, true
	}

	digest, hashedInfo, stable, err := digestFileWithLimit(path, maxSkillFileBytes)
	if err != nil {
		return StateUnavailable, false
	}
	if stable {
		b.mu.Lock()
		b.fileCache[path] = cachedFileDigest{
			size:       hashedInfo.Size(),
			modifiedNs: hashedInfo.ModTime().UnixNano(),
			sha256:     digest,
		}
		b.mu.Unlock()
	}
	return digest, true
}

func digestFile(path string) (string, error) {
	digest, _, _, err := digestFileWithLimit(path, 0)
	return digest, err
}

func digestFileWithLimit(path string, maxBytes int64) (string, os.FileInfo, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, false, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return "", nil, false, err
	}
	if before.IsDir() || (maxBytes > 0 && before.Size() > maxBytes) {
		return "", nil, false, errFileTooLarge
	}

	hash := sha256.New()
	reader := io.Reader(file)
	if maxBytes > 0 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	written, err := io.Copy(hash, reader)
	if err != nil {
		return "", nil, false, err
	}
	if maxBytes > 0 && written > maxBytes {
		return "", nil, false, errFileTooLarge
	}
	after, err := file.Stat()
	if err != nil {
		return "", nil, false, err
	}
	stable := before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
	return hex.EncodeToString(hash.Sum(nil)), after, stable, nil
}

func digestJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		// Every input type in this package is deliberately JSON-safe. Keep health
		// total if that contract is accidentally broken, and make the breakage
		// visible as a stable non-empty digest rather than returning raw data.
		encoded = []byte(StateUnavailable)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
