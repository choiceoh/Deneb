package briefcase

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolreg"
	chatfs "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/filesystem"
)

type FixtureRegistryConfig struct {
	Workspace  string
	World      *World
	Policy     *Policy
	Device     *DeviceTwin
	WikiStore  *wiki.Store
	ToolPolicy casepack.ToolPolicy
	Approval   ApprovalFunc
}

// NewFixtureRegistry builds a registry containing only case-local tools. The
// definitions for core filesystem tools come from production, while their
// executors are wrapped/replaced to enforce RunRoot and output-only mutation.
func NewFixtureRegistry(cfg FixtureRegistryConfig) (*chat.ToolRegistry, error) {
	if cfg.World == nil || cfg.Policy == nil || strings.TrimSpace(cfg.Workspace) == "" {
		return nil, errors.New("briefcase: workspace, world, and policy are required")
	}
	workspace, err := filepath.Abs(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("briefcase: resolve fixture workspace: %w", err)
	}
	if err := cfg.Policy.CheckRead(workspace); err != nil {
		return nil, fmt.Errorf("briefcase: fixture workspace policy: %w", err)
	}

	registry := chat.NewToolRegistry()
	advertised := make(map[string]struct{})
	register := func(def chat.ToolDef) {
		if !toolAdvertised(cfg.ToolPolicy, cfg.Approval, def.Name) {
			return
		}
		base := def.Fn
		def.Fn = func(ctx context.Context, input json.RawMessage) (string, error) {
			output, err := base(ctx, input)
			return sanitizeBriefcaseResult(workspace, output, err)
		}
		// Briefcase has a fully signed, small surface. Make allowed tools eager so
		// a denied fetch_tools rule cannot make an otherwise allowed tool unreachable.
		def.Deferred = false
		registry.RegisterTool(def)
		advertised[def.Name] = struct{}{}
	}
	fsDefs := chat.NewToolRegistry()
	toolreg.RegisterFSTools(fsDefs, &toolctx.CoreToolDeps{WorkspaceDir: workspace})
	for _, def := range fsDefs.Definitions() {
		switch def.Name {
		case "read":
			def.Fn = guardedRead(workspace, cfg.Policy)
		case "write":
			def.Fn = guardedOutputMutation(workspace, cfg.Policy, "write", chatfs.ToolWrite(workspace))
		case "edit":
			def.InputSchema = briefcaseEditSchema()
			def.Fn = guardedOutputMutation(workspace, cfg.Policy, "edit", chatfs.ToolEdit(workspace))
		case "grep":
			def.InputSchema = pureGrepFixtureSchema()
			def.Fn = pureGrep(workspace, cfg.Policy)
		default:
			continue
		}
		register(def)
	}

	for _, spec := range fixtureRecordSpecs() {
		register(chat.ToolDef{
			Name:        spec.name,
			Description: spec.description,
			InputSchema: recordFixtureSchema(),
			Fn:          recordFixture(cfg.World, spec.kinds),
		})
	}
	register(chat.ToolDef{
		Name:        "phone_write",
		Description: "Execute a scripted action against the Deneb-Briefcase Device Twin. No real device is contacted.",
		InputSchema: phoneWriteFixtureSchema(),
		Fn:          phoneWriteFixture(cfg.Policy, cfg.Device),
	})
	for _, rule := range cfg.ToolPolicy.Rules {
		if rule.Decision == casepack.ToolDeny {
			continue
		}
		if _, ok := advertised[rule.Name]; !ok {
			return nil, fmt.Errorf("briefcase: signed tool rule %q is not implemented by the case-local registry", rule.Name)
		}
	}
	return registry, nil
}

func toolAdvertised(policy casepack.ToolPolicy, approval ApprovalFunc, name string) bool {
	for _, rule := range policy.Rules {
		if rule.Name != name {
			continue
		}
		return rule.Decision == casepack.ToolAllow || (rule.Decision == casepack.ToolApproval && approval != nil)
	}
	return false
}

func fixtureToolSchemaDigest(registry *chat.ToolRegistry) (string, error) {
	preset := toolpreset.PresetBriefcase
	return toolSchemaDigest(
		registry,
		preset,
		toolpreset.AllowedTools(preset),
		toolpreset.PreloadedDeferredTools(preset),
	)
}

func toolSchemaDigest(registry *chat.ToolRegistry, preset toolpreset.Preset, allowed map[string]struct{}, preloaded []string) (string, error) {
	if registry == nil {
		return "", errors.New("briefcase: tool registry is required")
	}
	type digestTool struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
		Hidden      bool           `json:"hidden"`
		Deferred    bool           `json:"deferred"`
		Profile     string         `json:"profile,omitempty"`
		MaxOutput   int            `json:"maxOutput,omitempty"`
	}
	definitions := registry.FilteredDefinitions(allowed)
	tools := make([]digestTool, 0, len(definitions))
	for _, definition := range definitions {
		schema := definition.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		tools = append(tools, digestTool{
			Name: definition.Name, Description: definition.Description, InputSchema: schema,
			Hidden: definition.Hidden, Deferred: definition.Deferred, Profile: definition.Profile, MaxOutput: definition.MaxOutput,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	allowedNames := make([]string, 0, len(definitions))
	exposed := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		allowedNames = append(allowedNames, definition.Name)
		exposed[definition.Name] = struct{}{}
	}
	sort.Strings(allowedNames)
	filteredPreloaded := make([]string, 0, len(preloaded))
	for _, name := range preloaded {
		if _, ok := exposed[name]; ok {
			filteredPreloaded = append(filteredPreloaded, name)
		}
	}
	preloaded = filteredPreloaded
	sort.Strings(preloaded)
	data, err := json.Marshal(struct {
		Preset    string       `json:"preset"`
		Allowed   []string     `json:"allowed"`
		Preloaded []string     `json:"preloaded"`
		Tools     []digestTool `json:"tools"`
	}{Preset: string(preset), Allowed: allowedNames, Preloaded: preloaded, Tools: tools})
	if err != nil {
		return "", fmt.Errorf("briefcase: encode tool schema fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type recordFixtureSpec struct {
	name        string
	description string
	kinds       []casepack.SourceKind
}

func fixtureRecordSpecs() []recordFixtureSpec {
	return []recordFixtureSpec{
		{name: "mail_archive", description: "Search or read agent-visible mail records from this case only.", kinds: []casepack.SourceKind{casepack.SourceMail}},
		{name: "calendar", description: "Search or read agent-visible calendar records from this case only.", kinds: []casepack.SourceKind{casepack.SourceCalendar}},
		{name: "wiki", description: "Search or read agent-visible wiki and diary records from this case only.", kinds: []casepack.SourceKind{casepack.SourceWiki, casepack.SourceDiary}},
		{name: "knowledge", description: "Search grounded knowledge records visible in this case.", kinds: []casepack.SourceKind{casepack.SourceWiki, casepack.SourceDiary, casepack.SourceNotebook}},
		{name: "polaris", description: "Search visible transcript and workfeed memory records from this case.", kinds: []casepack.SourceKind{casepack.SourceTranscript, casepack.SourceWorkfeed}},
		{name: "notebook", description: "Search or read case-local notebook records.", kinds: []casepack.SourceKind{casepack.SourceNotebook}},
		{name: "files", description: "Search case-local files and captures.", kinds: []casepack.SourceKind{casepack.SourceFile, casepack.SourceCapture}},
		{name: "contacts", description: "Search case-local contact evidence.", kinds: []casepack.SourceKind{casepack.SourceFile}},
		{name: "todo", description: "Search case-local workfeed task evidence.", kinds: []casepack.SourceKind{casepack.SourceWorkfeed}},
		{name: "phone_read", description: "Read the scripted device state visible in this case.", kinds: []casepack.SourceKind{casepack.SourceDevice}},
	}
}

func recordFixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":       map[string]any{"type": "string"},
			"query":        map[string]any{"type": "string"},
			"id":           map[string]any{"type": "string"},
			"what":         map[string]any{"type": "string"},
			"offsetBytes":  map[string]any{"type": "integer", "minimum": 0},
			"limitBytes":   map[string]any{"type": "integer", "minimum": 1, "maximum": fixtureMaxRecordContent},
			"recordOffset": map[string]any{"type": "integer", "minimum": 0},
		},
		"additionalProperties": false,
	}
}

const (
	fixtureMaxRecords           = 4
	fixtureMaxRecordContent     = 8 << 10
	fixtureMaxQueryRecordBytes  = 2 << 10
	fixtureMaxTotalRecordOutput = 8 << 10
	fixtureMaxWireOutput        = 20 << 10
)

type fixtureWireRecord struct {
	ID           string                `json:"id"`
	Kind         casepack.SourceKind   `json:"kind"`
	Origin       casepack.SourceOrigin `json:"origin"`
	EventAt      string                `json:"eventAt"`
	AvailableAt  string                `json:"availableAt"`
	CapturedAt   string                `json:"capturedAt"`
	ProjectRefs  []string              `json:"projectRefs,omitempty"`
	SourceRef    string                `json:"sourceRef,omitempty"`
	Supersedes   []string              `json:"supersedes,omitempty"`
	Sensitivity  string                `json:"sensitivity,omitempty"`
	Memory       bool                  `json:"memory,omitempty"`
	Content      string                `json:"content"`
	ContentBytes int                   `json:"contentBytes"`
	Truncated    bool                  `json:"truncated,omitempty"`
	OffsetBytes  int                   `json:"offsetBytes,omitempty"`
	NextOffset   int                   `json:"nextOffset,omitempty"`
	Encoding     string                `json:"contentEncoding"`
}

type fixtureWireResponse struct {
	Count            int                 `json:"count"`
	TotalCount       int                 `json:"totalCount"`
	Limited          bool                `json:"limited,omitempty"`
	RecordOffset     int                 `json:"recordOffset,omitempty"`
	NextRecordOffset int                 `json:"nextRecordOffset,omitempty"`
	Records          []fixtureWireRecord `json:"records"`
}

func recordFixture(world *World, kinds []casepack.SourceKind) chat.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var params struct {
			Action       string `json:"action"`
			Query        string `json:"query"`
			ID           string `json:"id"`
			What         string `json:"what"`
			OffsetBytes  int    `json:"offsetBytes"`
			LimitBytes   int    `json:"limitBytes"`
			RecordOffset int    `json:"recordOffset"`
		}
		if len(bytes.TrimSpace(input)) > 0 {
			if err := decodeStrictFixtureInput(input, &params); err != nil {
				return "", fmt.Errorf("briefcase fixture input: %w", err)
			}
		}
		if params.OffsetBytes < 0 || params.RecordOffset < 0 {
			return "", errors.New("briefcase: offsets must not be negative")
		}
		if params.LimitBytes < 0 || params.LimitBytes > fixtureMaxRecordContent {
			return "", fmt.Errorf("briefcase: limitBytes must be between 1 and %d when set", fixtureMaxRecordContent)
		}
		query := params.Query
		if query == "" {
			query = params.What
		}
		var previews []RecordPreview
		totalCount := 0
		if params.ID != "" {
			if params.RecordOffset != 0 {
				return "", errors.New("briefcase: recordOffset is only valid for list/search")
			}
			limit := params.LimitBytes
			if limit == 0 {
				limit = fixtureMaxRecordContent
			}
			preview, err := world.GetPreviewRangeContext(ctx, params.ID, params.OffsetBytes, limit)
			if err != nil {
				return "", err
			}
			if !kindAllowed(preview.Source.Kind, kinds) {
				return "", fmt.Errorf("briefcase: source %q is not available through this tool", params.ID)
			}
			previews = []RecordPreview{preview}
			totalCount = 1
		} else {
			if params.OffsetBytes != 0 || params.LimitBytes != 0 {
				return "", errors.New("briefcase: offsetBytes and limitBytes require an id lookup")
			}
			queried, total, queryErr := world.QueryPreviewsContext(
				ctx, kinds, query, params.RecordOffset, fixtureMaxRecords, fixtureMaxQueryRecordBytes, fixtureMaxTotalRecordOutput,
			)
			if queryErr != nil {
				return "", queryErr
			}
			previews = queried
			totalCount = total
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		wire := make([]fixtureWireRecord, 0, len(previews))
		chunks := make([][]byte, 0, len(previews))
		for _, preview := range previews {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			record := preview.Record
			wireRecord := fixtureWireRecord{
				ID: record.Source.ID, Kind: record.Source.Kind,
				Origin:      record.Source.Origin,
				EventAt:     record.Source.EventAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				AvailableAt: record.Source.AvailableAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				CapturedAt:  record.Source.CapturedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				ProjectRefs: append([]string(nil), record.Source.ProjectRefs...),
				SourceRef:   record.Source.SourceRef, Supersedes: append([]string(nil), record.Source.Supersedes...),
				Sensitivity: record.Source.Sensitivity, Memory: record.Source.Memory,
				ContentBytes: preview.ContentBytes, OffsetBytes: preview.OffsetBytes,
			}
			chunk := append([]byte(nil), record.Content...)
			setFixtureWireContent(&wireRecord, chunk)
			wire = append(wire, wireRecord)
			chunks = append(chunks, chunk)
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		out, err := marshalBoundedFixtureResponse(ctx, params.ID != "", params.RecordOffset, totalCount, wire, chunks)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

func setFixtureWireContent(record *fixtureWireRecord, content []byte) {
	record.Content = string(content)
	record.Encoding = "utf-8"
	plain, _ := json.Marshal(record.Content)
	encoded := base64.StdEncoding.EncodeToString(content)
	encodedJSON, _ := json.Marshal(encoded)
	if !utf8.Valid(content) || len(encodedJSON) < len(plain) {
		record.Content = encoded
		record.Encoding = "base64"
	}
	end := record.OffsetBytes + len(content)
	record.Truncated = record.OffsetBytes > 0 || end < record.ContentBytes
	record.NextOffset = 0
	if end < record.ContentBytes {
		record.NextOffset = end
	}
}

func marshalBoundedFixtureResponse(ctx context.Context, single bool, recordOffset, totalCount int, wire []fixtureWireRecord, chunks [][]byte) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response := fixtureWireResponse{
			Count: len(wire), TotalCount: totalCount,
			Limited:          recordOffset+len(wire) < totalCount,
			RecordOffset:     recordOffset,
			NextRecordOffset: nextRecordOffset(recordOffset, len(wire), totalCount),
			Records:          wire,
		}
		out, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		if len(out) <= fixtureMaxWireOutput {
			return out, nil
		}

		shrink := -1
		for index := len(chunks) - 1; index >= 0; index-- {
			if len(chunks[index]) > 0 {
				shrink = index
				break
			}
		}
		if shrink >= 0 {
			remove := len(out) - fixtureMaxWireOutput
			if remove < 1 {
				remove = 1
			}
			newLength := len(chunks[shrink]) - remove
			if newLength < 0 {
				newLength = 0
			}
			if utf8.Valid(chunks[shrink]) {
				for newLength > 0 && !utf8.RuneStart(chunks[shrink][newLength]) {
					newLength--
				}
			}
			chunks[shrink] = chunks[shrink][:newLength]
			setFixtureWireContent(&wire[shrink], chunks[shrink])
			continue
		}
		if !single && len(wire) > 1 {
			wire = wire[:len(wire)-1]
			chunks = chunks[:len(chunks)-1]
			continue
		}
		return nil, fmt.Errorf("briefcase: fixture metadata exceeds the %d-byte response limit", fixtureMaxWireOutput)
	}
}

func nextRecordOffset(offset, count, total int) int {
	next := offset + count
	if count == 0 || next >= total {
		return 0
	}
	return next
}

func decodeStrictFixtureInput(input []byte, target any) error {
	if trimmed := bytes.TrimSpace(input); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("briefcase: tool input must be a JSON object")
	}
	if err := casepack.RejectDuplicateJSONKeys(input); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func kindAllowed(kind casepack.SourceKind, allowed []casepack.SourceKind) bool {
	for _, candidate := range allowed {
		if kind == candidate {
			return true
		}
	}
	return false
}

func guardedRead(workspace string, policy *Policy) chat.ToolFunc {
	base := chatfs.ToolRead(workspace)
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := rejectUnknownFixtureFields(input, "file_path", "offset", "limit", "function", "force", "hashes"); err != nil {
			return "", err
		}
		var params struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}
		path, err := resolveWorkspaceMember(workspace, params.FilePath)
		if err != nil {
			return "", err
		}
		if err := policy.CheckRead(path); err != nil {
			return "", err
		}
		input, err = rewriteWorkspacePathInput(workspace, input, "file_path", path)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		output, err := base(ctx, input)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return output, ctxErr
		}
		return sanitizeBriefcaseResult(workspace, output, err)
	}
}

func guardedOutputMutation(workspace string, policy *Policy, mode string, base chat.ToolFunc) chat.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := rejectMutationFixtureFields(input, mode); err != nil {
			return "", err
		}
		unlock := policy.lockMutation()
		defer unlock()
		var params struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}
		path, err := resolveWorkspaceMember(workspace, params.FilePath)
		if err != nil {
			return "", err
		}
		outputRoot := filepath.Join(workspace, "output")
		if !pathWithin(outputRoot, path) || path == outputRoot {
			return "", errors.New("briefcase: writes and edits are restricted to workspace/output")
		}
		limit, err := policy.WriteLimit(path)
		if err != nil {
			return "", err
		}
		if err := validateMutationSize(ctx, mode, input, path, limit); err != nil {
			return "", err
		}
		input, err = rewriteWorkspacePathInput(workspace, input, "file_path", path)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		output, err := base(ctx, input)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return output, ctxErr
		}
		if err == nil {
			info, statErr := os.Lstat(path)
			if statErr != nil || !info.Mode().IsRegular() || info.Size() > limit {
				return output, errors.New("briefcase: mutation violated the signed artifact size limit")
			}
		}
		return sanitizeBriefcaseResult(workspace, output, err)
	}
}

func pureGrep(workspace string, policy *Policy) chat.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := rejectUnknownFixtureFields(input, "pattern", "path", "ignoreCase", "maxResults"); err != nil {
			return "", err
		}
		var params struct {
			Pattern    string `json:"pattern"`
			Path       string `json:"path"`
			IgnoreCase bool   `json:"ignoreCase"`
			MaxResults int    `json:"maxResults"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}
		if params.Pattern == "" {
			return "", errors.New("pattern is required")
		}
		pattern := params.Pattern
		if params.IgnoreCase {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid grep pattern: %w", err)
		}
		root := workspace
		if params.Path != "" {
			root, err = resolveWorkspaceMember(workspace, params.Path)
			if err != nil {
				return "", err
			}
		}
		if err := policy.CheckRead(root); err != nil {
			return "", err
		}
		max := params.MaxResults
		if max <= 0 {
			max = 100
		}
		if max > 500 {
			max = 500
		}
		workspaceRoot, err := os.OpenRoot(workspace)
		if err != nil {
			return "", err
		}
		defer workspaceRoot.Close()
		rootRel, err := filepath.Rel(workspace, root)
		if err != nil {
			return "", err
		}
		errLimit := errors.New("briefcase: grep result limit reached")
		var matches []string
		var walk func(string) error
		walk = func(path string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(matches) >= max {
				return errLimit
			}
			info, err := workspaceRoot.Lstat(path)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("briefcase: grep encountered symlink %q", path)
			}
			if info.IsDir() {
				dir, err := workspaceRoot.Open(path)
				if err != nil {
					return err
				}
				entries, readErr := dir.ReadDir(-1)
				closeErr := dir.Close()
				if readErr != nil {
					return readErr
				}
				if closeErr != nil {
					return closeErr
				}
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				for _, entry := range entries {
					if err := ctx.Err(); err != nil {
						return err
					}
					child := entry.Name()
					if path != "." {
						child = filepath.Join(path, child)
					}
					if err := walk(child); err != nil {
						return err
					}
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("briefcase: grep encountered special file %q", path)
			}
			file, err := workspaceRoot.Open(path)
			if err != nil {
				return err
			}
			openedInfo, err := file.Stat()
			if err != nil {
				_ = file.Close()
				return err
			}
			if !openedInfo.Mode().IsRegular() {
				_ = file.Close()
				return fmt.Errorf("briefcase: grep path changed to a non-regular file %q", path)
			}
			scanner := bufio.NewScanner(file)
			line := 0
			for scanner.Scan() {
				if err := ctx.Err(); err != nil {
					_ = file.Close()
					return err
				}
				line++
				if re.MatchString(scanner.Text()) {
					matches = append(matches, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(path), line, scanner.Text()))
					if len(matches) >= max {
						break
					}
				}
			}
			scanErr := scanner.Err()
			closeErr := file.Close()
			if scanErr != nil {
				return scanErr
			}
			return closeErr
		}
		err = walk(rootRel)
		if err != nil && !errors.Is(err, errLimit) {
			return "", err
		}
		sort.Strings(matches)
		return strings.Join(matches, "\n"), nil
	}
}

func pureGrepFixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":    map[string]any{"type": "string"},
			"path":       map[string]any{"type": "string"},
			"ignoreCase": map[string]any{"type": "boolean"},
			"maxResults": map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func briefcaseEditSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path":  map[string]any{"type": "string"},
			"old_string": map[string]any{"type": "string"},
			"new_string": map[string]any{"type": "string"},
			"line":       map[string]any{"type": "integer", "minimum": 1},
			"anchor":     map[string]any{"type": "string"},
			"anchor_end": map[string]any{"type": "string"},
		},
		"required":             []string{"file_path", "new_string"},
		"additionalProperties": false,
	}
}

func phoneWriteFixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":     map[string]any{"type": "string"},
			"target": map[string]any{"type": "string"},
			"text":   map[string]any{"type": "string"},
			"title":  map[string]any{"type": "string"},
		},
		"required":             []string{"to"},
		"additionalProperties": false,
	}
}

func phoneWriteFixture(policy *Policy, device *DeviceTwin) chat.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if device == nil {
			return "", errors.New("briefcase: Device Twin is not configured")
		}
		if err := rejectUnknownFixtureFields(input, "to", "target", "text", "title"); err != nil {
			return "", err
		}
		var params struct {
			To string `json:"to"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}
		if err := policy.CheckDevice(params.To); err != nil {
			return "", err
		}
		id, err := DerivedDeviceActionID(params.To, input)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		result, err := device.PerformContext(ctx, DeviceAction{ActionID: id, Kind: params.To, Payload: input})
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return string(out), nil
	}
}

func rejectMutationFixtureFields(input json.RawMessage, mode string) error {
	if err := casepack.RejectDuplicateJSONKeys(input); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("briefcase: tool input must be a JSON object")
	}
	if _, ok := object["file_path"]; !ok {
		return errors.New("briefcase: file_path is required")
	}
	switch mode {
	case "write":
		if _, ok := object["content"]; !ok {
			return errors.New("briefcase: write content is required")
		}
		return rejectUnknownFixtureFields(input, "file_path", "content")
	case "edit":
		for _, forbidden := range []string{"replace_all", "regex", "edits"} {
			if _, present := object[forbidden]; present {
				return fmt.Errorf("briefcase: edit field %q is disabled by the bounded profile", forbidden)
			}
		}
		if _, batch := object["edits"]; !batch {
			if _, replacement := object["new_string"]; !replacement {
				return errors.New("briefcase: edit new_string or edits is required")
			}
		}
		return rejectUnknownFixtureFields(input,
			"file_path", "old_string", "new_string", "replace_all", "regex", "line", "anchor", "anchor_end", "edits")
	default:
		return fmt.Errorf("briefcase: unsupported mutation mode %q", mode)
	}
}

func validateMutationSize(ctx context.Context, mode string, input json.RawMessage, path string, limit int64) error {
	switch mode {
	case "write":
		var params struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return err
		}
		if int64(len(params.Content)) > limit {
			return fmt.Errorf("briefcase: write exceeds signed artifact limit %d", limit)
		}
		return nil
	case "edit":
		var params struct {
			NewString string `json:"new_string"`
			OldString string `json:"old_string"`
			Anchor    string `json:"anchor"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > limit || int64(len(params.NewString)) > limit-info.Size() {
			return fmt.Errorf("briefcase: edit may exceed signed artifact limit %d", limit)
		}
		if params.Anchor == "" {
			if params.OldString == "" {
				return errors.New("briefcase: edit requires exact old_string or anchor")
			}
			matched, err := fileContainsContext(ctx, path, []byte(params.OldString))
			if err != nil {
				return err
			}
			if !matched {
				return errors.New("briefcase: exact old_string was not found; whitespace-tolerant edit is disabled")
			}
		}
		return nil
	default:
		return fmt.Errorf("briefcase: unsupported mutation mode %q", mode)
	}
}

func fileContainsContext(ctx context.Context, path string, needle []byte) (bool, error) {
	if len(needle) == 0 {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	buffer := make([]byte, 64*1024)
	carry := make([]byte, 0, len(needle)-1)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			window := make([]byte, len(carry)+n)
			copy(window, carry)
			copy(window[len(carry):], buffer[:n])
			if bytes.Contains(window, needle) {
				return true, nil
			}
			keep := len(needle) - 1
			if keep > len(window) {
				keep = len(window)
			}
			carry = append(carry[:0], window[len(window)-keep:]...)
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

func rejectUnknownFixtureFields(input json.RawMessage, allowedNames ...string) error {
	if trimmed := bytes.TrimSpace(input); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("briefcase: tool input must be a JSON object")
	}
	if err := casepack.RejectDuplicateJSONKeys(input); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("briefcase: tool input must be a JSON object")
	}
	allowed := make(map[string]struct{}, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = struct{}{}
	}
	for name := range object {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("briefcase: unknown tool input field %q", name)
		}
	}
	return nil
}

func resolveWorkspaceMember(workspace, name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, '\x00') {
		return "", errors.New("briefcase: workspace path is required")
	}
	path := name
	const virtualWorkspace = "/briefcase/workspace"
	if path == virtualWorkspace {
		path = workspace
	} else if strings.HasPrefix(path, virtualWorkspace+"/") {
		path = filepath.Join(workspace, filepath.FromSlash(strings.TrimPrefix(path, virtualWorkspace+"/")))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, filepath.FromSlash(name))
	}
	path = filepath.Clean(path)
	if !pathWithin(workspace, path) {
		return "", fmt.Errorf("briefcase: path escapes workspace: %q", name)
	}
	return path, nil
}

func rewriteWorkspacePathInput(workspace string, input json.RawMessage, field, resolved string) (json.RawMessage, error) {
	relative, err := filepath.Rel(workspace, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("briefcase: cannot canonicalize workspace tool path")
	}
	if relative == "" {
		relative = "."
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	object[field] = encoded
	return json.Marshal(object)
}

func sanitizeBriefcaseResult(workspace, output string, err error) (string, error) {
	replacements := [][2]string{
		{filepath.Clean(workspace), "/briefcase/workspace"},
		{filepath.Clean(filepath.Dir(workspace)), "/briefcase"},
	}
	sanitize := func(value string) string {
		for _, pair := range replacements {
			value = strings.ReplaceAll(value, pair[0], pair[1])
			value = strings.ReplaceAll(value, filepath.ToSlash(pair[0]), pair[1])
		}
		return value
	}
	output = sanitize(output)
	if err != nil {
		return output, errors.New(sanitize(err.Error()))
	}
	return output, nil
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
