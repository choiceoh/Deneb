// discovery.go implements skill discovery from multiple source directories.
//
// Originally ported loadSkillEntries from the retired TypeScript skills package.
// Skills are loaded from 6 sources in precedence order:
//
//	extra < bundled < managed < agents-personal < agents-project < workspace
//
// Later sources override earlier ones by skill name.
package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// SkillsLimits controls safety caps during discovery.
type SkillsLimits struct {
	MaxCandidatesPerRoot     int
	MaxSkillsLoadedPerSource int
	MaxSkillsInPrompt        int
	MaxSkillsPromptChars     int
	MaxSkillFileBytes        int
}

// DefaultSkillsLimits returns the default limits matching the TypeScript implementation.
func DefaultSkillsLimits() SkillsLimits {
	return SkillsLimits{
		MaxCandidatesPerRoot:     300,
		MaxSkillsLoadedPerSource: 200,
		MaxSkillsInPrompt:        150,
		MaxSkillsPromptChars:     30_000,
		MaxSkillFileBytes:        256_000,
	}
}

// DiscoverConfig holds the configuration for skill discovery.
type DiscoverConfig struct {
	WorkspaceDir     string
	BundledSkillsDir string
	ManagedSkillsDir string
	ExtraDirs        []string
	PluginSkillDirs  []string // resolved plugin skill directories
	Limits           SkillsLimits
	Logger           *slog.Logger
}

func (c *DiscoverConfig) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *DiscoverConfig) limits() SkillsLimits {
	l := c.Limits
	if l.MaxCandidatesPerRoot <= 0 {
		l.MaxCandidatesPerRoot = 300
	}
	if l.MaxSkillsLoadedPerSource <= 0 {
		l.MaxSkillsLoadedPerSource = 200
	}
	if l.MaxSkillsInPrompt <= 0 {
		l.MaxSkillsInPrompt = 150
	}
	if l.MaxSkillsPromptChars <= 0 {
		l.MaxSkillsPromptChars = 30_000
	}
	if l.MaxSkillFileBytes <= 0 {
		l.MaxSkillFileBytes = 256_000
	}
	return l
}

// discoveredSkill is an intermediate type before creating SkillEntry.
type discoveredSkill struct {
	Name     string
	Desc     string
	FilePath string
	BaseDir  string
	Source   SkillSource
	Content  string // raw SKILL.md content (frontmatter only for progressive loading)
	Body     string // instruction body retained for exact-trigger JIT injection
	Category string // parent category directory name (empty for flat layout)
}

// DefaultManagedSkillsDir returns the managed skills catalog root under THIS
// process's state dir ({DENEB_STATE_DIR}/skills, else ~/.deneb/skills).
// Single source of truth for discovery and for the read tool's extra
// allowed root (tooldeps.CoreToolDeps.SkillsCatalogDir).
func DefaultManagedSkillsDir() string {
	stateDir := config.ResolveStateDir()
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "skills")
}

// DefaultPersonalSkillsDir returns the personal skills root (~/.agents/skills),
// or "" when the home directory cannot be resolved.
func DefaultPersonalSkillsDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".agents", "skills")
}

// DiscoverWorkspaceSkills discovers skills from all configured sources and
// returns merged entries with later sources overriding earlier ones by name.
func DiscoverWorkspaceSkills(cfg DiscoverConfig) []SkillEntry {
	log := cfg.logger()
	limits := cfg.limits()
	home, _ := os.UserHomeDir()

	// Resolve source directories.
	managedDir := cfg.ManagedSkillsDir
	if managedDir == "" {
		managedDir = DefaultManagedSkillsDir()
	}
	workspaceSkillsDir := filepath.Join(cfg.WorkspaceDir, "skills")

	// Merge extra + plugin dirs.
	mergedExtraDirs := make([]string, 0, len(cfg.ExtraDirs)+len(cfg.PluginSkillDirs))
	for _, d := range cfg.ExtraDirs {
		d = strings.TrimSpace(d)
		if d != "" {
			mergedExtraDirs = append(mergedExtraDirs, d)
		}
	}
	mergedExtraDirs = append(mergedExtraDirs, cfg.PluginSkillDirs...)

	// Load from each source.
	extraSkills := make([]discoveredSkill, 0)
	for _, dir := range mergedExtraDirs {
		extraSkills = append(extraSkills, loadSkillsFromSource(dir, SourceExtra, limits, log)...)
	}

	bundledSkills := loadSkillsFromSource(cfg.BundledSkillsDir, SourceBundled, limits, log)

	managedSkills := loadSkillsFromSource(managedDir, SourceManaged, limits, log)

	// The genesis output dir nests one level deeper than this walker reaches
	// (managed/genesis/<category>/<name>/SKILL.md — the managed walk treats
	// "genesis" as a category and finds no SKILL.md one level down), so load
	// it as its own source root, inside which the layout is the standard
	// category nesting. Without this, loop-generated skills exist on disk but
	// vanish from the catalog, the system prompt, and the skills tab at every
	// restart (they were only ever visible via the in-memory Register at
	// creation time).
	var genesisSkills []discoveredSkill
	if managedDir != "" {
		genesisSkills = loadSkillsFromSource(filepath.Join(managedDir, "genesis"), SourceManaged, limits, log)
	}

	var personalSkills []discoveredSkill
	if home != "" {
		personalDir := filepath.Join(home, ".agents", "skills")
		personalSkills = loadSkillsFromSource(personalDir, SourcePersonal, limits, log)
	}

	projectDir := filepath.Join(cfg.WorkspaceDir, ".agents", "skills")
	projectSkills := loadSkillsFromSource(projectDir, SourceProject, limits, log)

	workspaceSkills := loadSkillsFromSource(workspaceSkillsDir, SourceWorkspace, limits, log)

	// Merge by name: extra < bundled < managed (incl. genesis) < personal <
	// project < workspace
	merged := make(map[string]discoveredSkill)
	for _, s := range extraSkills {
		merged[s.Name] = s
	}
	for _, s := range bundledSkills {
		merged[s.Name] = s
	}
	for _, s := range managedSkills {
		merged[s.Name] = s
	}
	for _, s := range genesisSkills {
		merged[s.Name] = s
	}
	for _, s := range personalSkills {
		merged[s.Name] = s
	}
	for _, s := range projectSkills {
		merged[s.Name] = s
	}
	for _, s := range workspaceSkills {
		merged[s.Name] = s
	}

	// Convert to SkillEntry with parsed frontmatter/metadata.
	entries := make([]SkillEntry, 0, len(merged))
	for _, ds := range merged {
		fm := ParseFrontmatter(ds.Content)
		entry := SkillEntry{
			Skill: Skill{
				Name:     ds.Name,
				Dir:      ds.BaseDir,
				Source:   ds.Source,
				Category: ds.Category,
			},
			Frontmatter: fm,
			Metadata:    ResolveDenebMetadata(fm),
			Invocation:  ptrInvocationPolicy(ResolveSkillInvocationPolicy(fm)),
			Body:        ds.Body,
		}
		// Resolve skill type from frontmatter (default: prompt).
		if t, ok := fm["type"]; ok && IsValidSkillType(t) {
			entry.Skill.Type = SkillType(t)
		} else {
			entry.Skill.Type = SkillTypePrompt
		}
		// Use description from frontmatter if available, else from SKILL.md parsing.
		if desc, ok := fm["description"]; ok && desc != "" {
			entry.Skill.Description = desc
		} else if ds.Desc != "" {
			entry.Skill.Description = ds.Desc
		}
		// Version from frontmatter.
		if v, ok := fm["version"]; ok && v != "" {
			entry.Skill.Version = v
		}
		// Category from frontmatter overrides directory-based category.
		if cat, ok := fm["category"]; ok && cat != "" {
			entry.Skill.Category = cat
		}
		// Store file path for prompt building.
		entry.Skill.FilePath = ds.FilePath
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Skill.Name < entries[j].Skill.Name
	})
	return entries
}

func ptrInvocationPolicy(p SkillInvocationPolicy) *SkillInvocationPolicy {
	return &p
}

// LoadSkillEntry loads a single skill directory (dir/SKILL.md) into a fully
// parsed SkillEntry — the per-directory unit of DiscoverWorkspaceSkills,
// exposed for consumers that materialize ONE skill outside a discovery walk
// (the evolver's bundled-skill adoption copies a repo skill into the managed
// dir and needs its entry immediately, without a full re-discovery).
// Category comes from frontmatter only (there is no walk context here); the
// name falls back to the directory basename when frontmatter omits it.
func LoadSkillEntry(dir string, source SkillSource) (*SkillEntry, error) {
	filePath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	fm := ParseFrontmatter(string(data))
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		name = filepath.Base(dir)
	}
	entry := SkillEntry{
		Skill: Skill{
			Name:     name,
			Dir:      dir,
			FilePath: filePath,
			Source:   source,
		},
		Frontmatter: fm,
		Metadata:    ResolveDenebMetadata(fm),
		Invocation:  ptrInvocationPolicy(ResolveSkillInvocationPolicy(fm)),
		Body:        jitSkillInstructionBody(string(data)),
	}
	if t, ok := fm["type"]; ok && IsValidSkillType(t) {
		entry.Skill.Type = SkillType(t)
	} else {
		entry.Skill.Type = SkillTypePrompt
	}
	if desc, ok := fm["description"]; ok && desc != "" {
		entry.Skill.Description = desc
	}
	if v, ok := fm["version"]; ok && v != "" {
		entry.Skill.Version = v
	}
	if cat, ok := fm["category"]; ok && cat != "" {
		entry.Skill.Category = cat
	}
	return &entry, nil
}

// loadSkillsFromSource loads skills from a single directory with limits.
func loadSkillsFromSource(dir string, source SkillSource, limits SkillsLimits, log *slog.Logger) []discoveredSkill {
	if dir == "" {
		return nil
	}
	dir = filepath.Clean(dir)

	rootRealPath, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}

	// Detect nested skills root (dir/skills/*/SKILL.md).
	baseDir := resolveNestedSkillsRoot(dir, limits.MaxCandidatesPerRoot)
	baseDirReal := resolveContainedPath(baseDir, dir, rootRealPath)
	if baseDirReal == "" {
		return nil
	}

	// Check if the root itself is a single skill directory.
	rootSkillMd := filepath.Join(baseDir, "SKILL.md")
	if fileExists(rootSkillMd) {
		return loadRootSkill(rootSkillMd, baseDir, dir, rootRealPath, source, limits, log)
	}

	// List child directories.
	childDirs := listChildDirectories(baseDir)
	if len(childDirs) > limits.MaxCandidatesPerRoot {
		log.Warn("skills root suspiciously large, truncating",
			"dir", baseDir, "count", len(childDirs), "max", limits.MaxCandidatesPerRoot)
	}
	sort.Strings(childDirs)
	maxCandidates := limits.MaxSkillsLoadedPerSource
	if len(childDirs) > maxCandidates {
		childDirs = childDirs[:maxCandidates]
	}

	var loaded []discoveredSkill
	for _, name := range childDirs {
		skillDir := filepath.Join(baseDir, name)
		skillDirReal := resolveContainedPath(skillDir, dir, rootRealPath)
		if skillDirReal == "" {
			continue
		}
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if fileExists(skillMd) {
			// Flat layout: skills/skill-name/SKILL.md
			ds := loadSingleSkill(skillMd, skillDir, dir, rootRealPath, "", source, limits, log)
			if ds != nil {
				loaded = append(loaded, *ds)
			}
		} else {
			// Nested category layout: skills/category/skill-name/SKILL.md
			loaded = loadNestedCategorySkills(loaded, skillDir, name, dir, rootRealPath, source, limits, log)
		}
		if len(loaded) >= limits.MaxSkillsLoadedPerSource {
			break
		}
	}
	return loaded
}

// loadRootSkill loads a skills root that is itself a single skill directory
// (baseDir/SKILL.md, no child walk). Unlike loadSingleSkill it keeps the FULL
// file content (a lone root skill has no progressive-loading pressure).
// Returns nil when the file escapes the root, is oversized, or is unreadable.
func loadRootSkill(rootSkillMd, baseDir, rootDir, rootRealPath string, source SkillSource, limits SkillsLimits, log *slog.Logger) []discoveredSkill {
	skillMdReal := resolveContainedPath(rootSkillMd, rootDir, rootRealPath)
	if skillMdReal == "" {
		return nil
	}
	size := fileSize(skillMdReal)
	if size > int64(limits.MaxSkillFileBytes) {
		log.Warn("skipping skills root: oversized SKILL.md",
			"dir", baseDir, "size", size, "max", limits.MaxSkillFileBytes)
		return nil
	}
	content, err := os.ReadFile(skillMdReal)
	if err != nil {
		return nil
	}
	raw := string(content)
	name, desc := extractSkillNameAndDesc(raw, filepath.Base(baseDir))
	return []discoveredSkill{{
		Name:     name,
		Desc:     desc,
		FilePath: rootSkillMd,
		BaseDir:  baseDir,
		Source:   source,
		Content:  raw,
		Body:     jitSkillInstructionBody(raw),
	}}
}

// loadNestedCategorySkills walks one category directory of the nested layout
// (skills/category/skill-name/SKILL.md), appending each loaded skill to loaded
// in sorted-subdirectory order until the per-source cap is reached. Returns
// the (possibly grown) slice.
func loadNestedCategorySkills(loaded []discoveredSkill, categoryDir, category, rootDir, rootRealPath string, source SkillSource, limits SkillsLimits, log *slog.Logger) []discoveredSkill {
	subDirs := listChildDirectories(categoryDir)
	sort.Strings(subDirs)
	for _, subName := range subDirs {
		subSkillDir := filepath.Join(categoryDir, subName)
		subSkillDirReal := resolveContainedPath(subSkillDir, rootDir, rootRealPath)
		if subSkillDirReal == "" {
			continue
		}
		subSkillMd := filepath.Join(subSkillDir, "SKILL.md")
		if !fileExists(subSkillMd) {
			continue
		}
		ds := loadSingleSkill(subSkillMd, subSkillDir, rootDir, rootRealPath, category, source, limits, log)
		if ds != nil {
			loaded = append(loaded, *ds)
			if len(loaded) >= limits.MaxSkillsLoadedPerSource {
				break
			}
		}
	}
	return loaded
}

// loadSingleSkill loads a single skill from its SKILL.md file.
// Uses progressive loading: only reads the frontmatter block for metadata
// extraction, deferring the full body read to when the LLM requests it.
func loadSingleSkill(skillMdPath, skillDir, rootDir, rootRealPath, category string, source SkillSource, limits SkillsLimits, log *slog.Logger) *discoveredSkill {
	skillMdReal := resolveContainedPath(skillMdPath, rootDir, rootRealPath)
	if skillMdReal == "" {
		return nil
	}
	size := fileSize(skillMdReal)
	if size > int64(limits.MaxSkillFileBytes) {
		log.Warn("skipping skill: oversized SKILL.md",
			"skill", filepath.Base(skillDir), "size", size, "max", limits.MaxSkillFileBytes)
		return nil
	}
	content, err := os.ReadFile(skillMdReal)
	if err != nil {
		return nil
	}

	// Progressive loading: metadata keeps only the frontmatter block. The
	// instruction body is retained separately for bounded exact-trigger JIT
	// injection, never the ambient prompt.
	raw := string(content)
	header, _ := ExtractFrontmatterBlock(raw)
	if header == "" {
		header = raw
	}

	skillName, desc := extractSkillNameAndDesc(raw, filepath.Base(skillDir))
	return &discoveredSkill{
		Name:     skillName,
		Desc:     desc,
		FilePath: skillMdPath,
		BaseDir:  skillDir,
		Source:   source,
		Content:  header,
		Body:     jitSkillInstructionBody(raw),
		Category: category,
	}
}

// jitSkillInstructionBody retains bodies only for model-invocable prompt
// skills with explicit triggers. This keeps the frozen snapshot proportional
// to the small auto-load roster rather than every discovered SKILL.md.
func jitSkillInstructionBody(content string) string {
	fm := ParseFrontmatter(content)
	metadata := ResolveDenebMetadata(fm)
	if metadata == nil || len(metadata.Triggers) == 0 || ResolveSkillInvocationPolicy(fm).DisableModelInvocation {
		return ""
	}
	if skillType, ok := fm["type"]; ok && IsValidSkillType(skillType) && SkillType(skillType) != SkillTypePrompt {
		return ""
	}
	_, offset := ExtractFrontmatterBlock(content)
	if offset > 0 && offset < len(content) {
		return operationalSkillBody(content[offset:])
	}
	if offset >= len(content) {
		return ""
	}
	return operationalSkillBody(content)
}

// operationalSkillBody drops the terminal changelog: version history is
// useful to reviewers but never an execution instruction. Earlier changelog
// examples remain intact because only the last section is removed.
func operationalSkillBody(body string) string {
	body = strings.TrimSpace(body)
	const marker = "\n## Changelog"
	if i := strings.LastIndex(body, marker); i >= 0 && !strings.Contains(body[i+len(marker):], "\n## ") {
		body = strings.TrimSpace(body[:i])
	}
	return body
}

// resolveNestedSkillsRoot detects if dir has a nested skills/ subdirectory
// that actually contains skills (dir/skills/*/SKILL.md).
func resolveNestedSkillsRoot(dir string, maxScan int) string {
	nested := filepath.Join(dir, "skills")
	info, err := os.Stat(nested)
	if err != nil || !info.IsDir() {
		return dir
	}
	entries, err := os.ReadDir(nested)
	if err != nil {
		return dir
	}
	scanLimit := maxScan
	if scanLimit <= 0 {
		scanLimit = 100
	}
	scanned := 0
	for _, entry := range entries {
		if scanned >= scanLimit {
			break
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		scanned++
		skillMd := filepath.Join(nested, entry.Name(), "SKILL.md")
		if fileExists(skillMd) {
			return nested
		}
	}
	return dir
}

// resolveContainedPath checks that candidatePath resolves within rootDir (symlink escape prevention).
func resolveContainedPath(candidatePath, _, rootRealPath string) string {
	realPath, err := filepath.EvalSymlinks(candidatePath)
	if err != nil {
		return ""
	}
	if isPathInside(rootRealPath, realPath) {
		return realPath
	}
	return ""
}

// isPathInside checks if child is inside parent.
func isPathInside(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	prefix := parent + string(filepath.Separator)
	return strings.HasPrefix(child, prefix)
}

// listChildDirectories returns names of child directories (skips dotfiles and node_modules).
func listChildDirectories(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if entry.IsDir() {
			dirs = append(dirs, name)
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(fullPath)
			if err == nil && info.IsDir() {
				dirs = append(dirs, name)
			}
		}
	}
	return dirs
}

// extractSkillNameAndDesc extracts the skill name and description from SKILL.md content.
// Uses the frontmatter "name" field if present, otherwise the directory name.
// Description comes from the frontmatter "description" field.
func extractSkillNameAndDesc(content, dirName string) (name, desc string) {
	fm := ParseFrontmatter(content)
	if n, ok := fm["name"]; ok && strings.TrimSpace(n) != "" {
		name = strings.TrimSpace(n)
	} else {
		name = dirName
	}
	if d, ok := fm["description"]; ok {
		desc = strings.TrimSpace(d)
	}
	return name, desc
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
