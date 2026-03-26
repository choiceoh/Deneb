package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestIsExtensionFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"foo.ts", true},
		{"foo.js", true},
		{"foo.mts", true},
		{"foo.cts", true},
		{"foo.mjs", true},
		{"foo.cjs", true},
		{"foo.d.ts", false},
		{"foo.go", false},
		{"foo.py", false},
		{"foo", false},
		{"foo.json", false},
	}
	for _, tt := range tests {
		got := isExtensionFile(tt.path)
		if got != tt.want {
			t.Errorf("isExtensionFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestShouldIgnoreScannedDirectory(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"node_modules", false},
		{"my-plugin", false},
		{"old-plugin.bak", true},
		{"plugin.backup-2024", true},
		{"plugin.disabled", true},
		{"", true},
		{"  ", true},
		{"PLUGIN.BAK", true},
		{"test.DISABLED", true},
	}
	for _, tt := range tests {
		got := shouldIgnoreScannedDirectory(tt.name)
		if got != tt.want {
			t.Errorf("shouldIgnoreScannedDirectory(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDeriveIDHint(t *testing.T) {
	tests := []struct {
		filePath        string
		packageName     string
		hasMultipleExts bool
		want            string
	}{
		{"index.ts", "", false, "index"},
		{"index.ts", "my-plugin", false, "my-plugin"},
		{"channel.ts", "@deneb/voice-call", false, "voice-call"},
		{"channel.ts", "@deneb/voice-call", true, "voice-call/channel"},
		{"index.ts", "my-provider", false, "my"},
		{"foo.ts", "simple", true, "simple/foo"},
	}
	for _, tt := range tests {
		got := deriveIDHint(tt.filePath, tt.packageName, tt.hasMultipleExts)
		if got != tt.want {
			t.Errorf("deriveIDHint(%q, %q, %v) = %q, want %q",
				tt.filePath, tt.packageName, tt.hasMultipleExts, got, tt.want)
		}
	}
}

func TestIsPathInside(t *testing.T) {
	tests := []struct {
		root   string
		target string
		want   bool
	}{
		{"/home/user/plugins", "/home/user/plugins/foo", true},
		{"/home/user/plugins", "/home/user/plugins/foo/bar", true},
		{"/home/user/plugins", "/home/user/other", false},
		{"/home/user/plugins", "/home/user", false},
		{"/home/user/plugins", "/home/user/plugins", true},
	}
	for _, tt := range tests {
		got := isPathInside(tt.root, tt.target)
		if got != tt.want {
			t.Errorf("isPathInside(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
		}
	}
}

func TestPluginDiscovererClearCache(t *testing.T) {
	d := NewPluginDiscoverer(slog.Default())
	d.cache["test"] = &discoveryEntry{result: &PluginDiscoveryResult{}}
	if len(d.cache) != 1 {
		t.Fatal("expected 1 cache entry")
	}
	d.ClearCache()
	if len(d.cache) != 0 {
		t.Fatal("expected 0 cache entries after clear")
	}
}

func TestDiscoverPluginsEmptyRoots(t *testing.T) {
	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots: PluginSourceRoots{},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}
}

func TestDiscoverPluginsFromDirectory(t *testing.T) {
	// Create a temp directory with a plugin.
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write an index.ts.
	if err := os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots: PluginSourceRoots{
			Global: tmpDir,
		},
		NoCache: true,
	})

	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}
	if result.Candidates[0].IDHint != "my-plugin" {
		t.Errorf("expected IDHint 'my-plugin', got %q", result.Candidates[0].IDHint)
	}
	if result.Candidates[0].Origin != OriginGlobal {
		t.Errorf("expected origin 'global', got %q", result.Candidates[0].Origin)
	}
}

func TestDiscoverPluginsWithPackageJSON(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "my-pkg")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := `{
		"name": "@deneb/voice-call",
		"version": "1.0.0",
		"description": "Voice call plugin",
		"deneb": {
			"plugin": {
				"id": "voice-call",
				"channels": ["telegram"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots:   PluginSourceRoots{Global: tmpDir},
		NoCache: true,
	})

	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}
	c := result.Candidates[0]
	if c.PackageName != "@deneb/voice-call" {
		t.Errorf("expected package name '@deneb/voice-call', got %q", c.PackageName)
	}
	if c.PackageVersion != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", c.PackageVersion)
	}
	if c.PackageManifest == nil || c.PackageManifest.ID != "voice-call" {
		t.Errorf("expected manifest ID 'voice-call', got %v", c.PackageManifest)
	}
}

func TestDiscoverPluginsWithExtensions(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "multi")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := `{
		"name": "multi-plugin",
		"version": "1.0.0",
		"deneb": {
			"extensions": ["ext-a.ts", "ext-b.ts"]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "ext-a.ts"), []byte("// a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "ext-b.ts"), []byte("// b"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots:   PluginSourceRoots{Global: tmpDir},
		NoCache: true,
	})

	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.Candidates))
	}
}

func TestDiscoverPluginsDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "dedup")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	// Discover the same root twice (via workspace and global).
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots: PluginSourceRoots{
			Workspace: tmpDir,
			Global:    tmpDir,
		},
		WorkspaceDir: tmpDir,
		NoCache:      true,
	})

	// Should only appear once due to path-based dedup.
	if len(result.Candidates) != 1 {
		t.Errorf("expected 1 candidate (deduped), got %d", len(result.Candidates))
	}
}

func TestDiscoverPluginsIgnoresBakDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	bakDir := filepath.Join(tmpDir, "old-plugin.bak")
	if err := os.MkdirAll(bakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bakDir, "index.ts"), []byte("// bak"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots:   PluginSourceRoots{Global: tmpDir},
		NoCache: true,
	})

	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates (ignored .bak), got %d", len(result.Candidates))
	}
}

func TestDiscoverPluginsBundleFormat(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "bundled")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := `{"id": "my-bundle", "version": "1.0.0"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "deneb-plugin.json"), []byte(bundle), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots:   PluginSourceRoots{Stock: tmpDir},
		NoCache: true,
	})

	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}
	c := result.Candidates[0]
	if c.IDHint != "my-bundle" {
		t.Errorf("expected IDHint 'my-bundle', got %q", c.IDHint)
	}
	if c.Format != FormatBundle {
		t.Errorf("expected format 'bundle', got %q", c.Format)
	}
	if c.Origin != OriginBundled {
		t.Errorf("expected origin 'bundled', got %q", c.Origin)
	}
}

func TestDiscoverPluginsCaching(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "cached-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte("// plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	params := DiscoverPluginsParams{
		Roots: PluginSourceRoots{Global: tmpDir},
	}

	result1 := d.DiscoverPlugins(params)
	result2 := d.DiscoverPlugins(params)

	// Should get the same pointer from cache.
	if result1 != result2 {
		t.Error("expected same cached result on second call")
	}

	// Verify NoCache bypasses.
	params.NoCache = true
	result3 := d.DiscoverPlugins(params)
	if result3 == result1 {
		t.Error("expected different result with NoCache=true")
	}
}

func TestDiscoverPluginsFromExtraPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a single plugin file.
	pluginFile := filepath.Join(tmpDir, "my-extension.ts")
	if err := os.WriteFile(pluginFile, []byte("// ext"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		ExtraPaths: []string{pluginFile},
		NoCache:    true,
	})

	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}
	if result.Candidates[0].Origin != OriginConfig {
		t.Errorf("expected origin 'config', got %q", result.Candidates[0].Origin)
	}
	if result.Candidates[0].IDHint != "my-extension" {
		t.Errorf("expected IDHint 'my-extension', got %q", result.Candidates[0].IDHint)
	}
}

func TestDiscoverPluginsOriginPrecedence(t *testing.T) {
	// Create two roots with overlapping but different plugins.
	workDir := t.TempDir()
	globalDir := t.TempDir()

	// Workspace plugin.
	wpDir := filepath.Join(workDir, "plugin-a")
	os.MkdirAll(wpDir, 0o755)
	os.WriteFile(filepath.Join(wpDir, "index.ts"), []byte("// workspace"), 0o644)

	// Global plugin.
	gpDir := filepath.Join(globalDir, "plugin-b")
	os.MkdirAll(gpDir, 0o755)
	os.WriteFile(filepath.Join(gpDir, "index.ts"), []byte("// global"), 0o644)

	d := NewPluginDiscoverer(slog.Default())
	result := d.DiscoverPlugins(DiscoverPluginsParams{
		Roots: PluginSourceRoots{
			Workspace: workDir,
			Global:    globalDir,
		},
		WorkspaceDir: workDir,
		NoCache:      true,
	})

	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.Candidates))
	}

	// Workspace should come first.
	if result.Candidates[0].Origin != OriginWorkspace {
		t.Errorf("expected first candidate origin 'workspace', got %q", result.Candidates[0].Origin)
	}
	if result.Candidates[1].Origin != OriginGlobal {
		t.Errorf("expected second candidate origin 'global', got %q", result.Candidates[1].Origin)
	}
}

func TestFormatCandidateBlockMessage(t *testing.T) {
	tests := []struct {
		issue *candidateBlockIssue
		want  string
	}{
		{
			&candidateBlockIssue{reason: BlockSourceEscapesRoot, sourcePath: "/a", rootRealPath: "/b", sourceRealPath: "/c"},
			"blocked plugin candidate: source escapes plugin root (/a -> /c; root=/b)",
		},
		{
			&candidateBlockIssue{reason: BlockPathStatFailed, targetPath: "/missing"},
			"blocked plugin candidate: cannot stat path (/missing)",
		},
		{
			&candidateBlockIssue{reason: BlockPathWorldWritable, targetPath: "/writable", modeBits: 0o777},
			"blocked plugin candidate: world-writable path (/writable, mode=0777)",
		},
		{
			&candidateBlockIssue{reason: BlockSuspiciousOwnership, targetPath: "/owned", foundUID: 1000, expectedUID: 500},
			"blocked plugin candidate: suspicious ownership (/owned, uid=1000, expected uid=500 or root)",
		},
	}
	for _, tt := range tests {
		got := formatCandidateBlockMessage(tt.issue)
		if got != tt.want {
			t.Errorf("formatCandidateBlockMessage() = %q, want %q", got, tt.want)
		}
	}
}
