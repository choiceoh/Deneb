// discovery.go implements skill discovery from multiple source directories.
//
// This ports src/agents/skills/workspace.ts:loadSkillEntries() to Go.
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
	Category string // parent category directory name (empty for flat layout)
}

// DiscoverWorkspaceSkills discovers skills from all configured sources and
// returns merged entries with later sources overriding earlier ones by name.
func DiscoverWorkspaceSkills(cfg DiscoverConfig) []SkillEntry {
	log := cfg.logger()
	limits := cfg.limits()
	home, _ := os.UserHomeDir()

	// Resolve source directories.
	managedDir := cfg.ManagedSkillsDir
	if managedDir == "" && home != "" {
		managedDir = filepath.Join(home, ".deneb", "skills")
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

	var personalSkills []discoveredSkill
	if home != "" {
		personalDir := filepath.Join(home, ".agents", "skills")
		personalSkills = loadSkillsFromSource(personalDir, SourcePersonal, limits, log)
	}

	projectDir := filepath.Join(cfg.WorkspaceDir, ".agents", "skills")
	projectSkills := loadSkillsFromSource(projectDir, SourceProject, limits, log)

	workspaceSkills := loadSkillsFromSource(workspaceSkillsDir, SourceWorkspace, limits, log)

	// Merge by name: extra < bundled < managed < personal < project < workspace
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
		skillMdReal := resolveContainedPath(rootSkillMd, dir, rootRealPath)
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
		name, desc := extractSkillNameAndDesc(string(content), filepath.Base(baseDir))
		return []discoveredSkill{{
			Name:     name,
			Desc:     desc,
			FilePath: rootSkillMd,
			BaseDir:  baseDir,
			Source:   source,
			Content:  string(content),
		}}
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
				if len(loaded) >= limits.MaxSkillsLoadedPerSource {
					break
				}
			}
			continue
		}

		// Nested category layout: skills/category/skill-name/SKILL.md
		// Check if this directory contains subdirectories with SKILL.md files.
		subDirs := listChildDirectories(skillDir)
		sort.Strings(subDirs)
		for _, subName := range subDirs {
			subSkillDir := filepath.Join(skillDir, subName)
			subSkillDirReal := resolveContainedPath(subSkillDir, dir, rootRealPath)
			if subSkillDirReal == "" {
				continue
			}
			subSkillMd := filepath.Join(subSkillDir, "SKILL.md")
			if !fileExists(subSkillMd) {
				continue
			}
			ds := loadSingleSkill(subSkillMd, subSkillDir, dir, rootRealPath, name, source, limits, log)
			if ds != nil {
				loaded = append(loaded, *ds)
				if len(loaded) >= limits.MaxSkillsLoadedPerSource {
					break
				}
			}
		}
		if len(loaded) >= limits.MaxSkillsLoadedPerSource {
			break
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

	// Progressive loading: extract only the frontmatter block for metadata.
	// The full body is read on demand by the LLM via the file path.
	header, _ := ExtractFrontmatterBlock(string(content))
	if header == "" {
		header = string(content)
	}

	skillName, desc := extractSkillNameAndDesc(string(content), filepath.Base(skillDir))
	return &discoveredSkill{
		Name:     skillName,
		Desc:     desc,
		FilePath: skillMdPath,
		BaseDir:  skillDir,
		Source:   source,
		Content:  header,
		Category: category,
	}
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
func resolveContainedPath(candidatePath, rootDir, rootRealPath string) string {
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
