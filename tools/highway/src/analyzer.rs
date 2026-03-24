//! Import graph analyzer using oxc_parser for native-speed TypeScript parsing.
//!
//! Builds a complete dependency graph of the project by parsing every TS/JS file
//! and extracting import/export declarations. Given a set of changed files,
//! computes the transitive closure of affected test files.

use std::collections::{HashMap, HashSet, VecDeque};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use dashmap::DashMap;
use ignore::WalkBuilder;
use memchr::memmem;
use oxc_allocator::Allocator;
use oxc_ast::ast::Statement;
use oxc_parser::Parser;
use oxc_resolver::{ResolveOptions, Resolver};
use oxc_span::SourceType;
use rayon::prelude::*;

/// Represents the full import graph of the project.
#[derive(Debug)]
pub struct ImportGraph {
    /// file -> set of files it imports (forward edges)
    pub forward: HashMap<PathBuf, HashSet<PathBuf>>,
    /// file -> set of files that import it (reverse edges)
    pub reverse: HashMap<PathBuf, HashSet<PathBuf>>,
    /// all discovered test files
    pub test_files: Vec<PathBuf>,
}

/// Configuration for the import graph analyzer.
pub struct AnalyzerConfig {
    pub root: PathBuf,
    pub extensions: Vec<String>,
    pub ignore_dirs: Vec<String>,
    pub test_pattern: String,
}

impl Default for AnalyzerConfig {
    fn default() -> Self {
        Self {
            root: PathBuf::from("."),
            extensions: vec![
                "ts".into(),
                "tsx".into(),
                "js".into(),
                "jsx".into(),
                "mts".into(),
                "mjs".into(),
            ],
            ignore_dirs: vec![
                "node_modules".into(),
                "dist".into(),
                "dist-runtime".into(),
                ".git".into(),
                "coverage".into(),
            ],
            test_pattern: ".test.".into(),
        }
    }
}

/// Build the complete import graph by scanning and parsing all source files.
pub fn build_import_graph(config: &AnalyzerConfig) -> Result<ImportGraph> {
    let root = config.root.canonicalize().context("canonicalize root")?;

    // Phase 1: Discover all source files in parallel
    let source_files = discover_source_files(&root, &config.extensions, &config.ignore_dirs);

    // Phase 2: Parse all files and extract imports in parallel using rayon
    let imports_map: DashMap<PathBuf, Vec<String>> = DashMap::new();
    let test_files: DashMap<PathBuf, ()> = DashMap::new();

    source_files.par_iter().for_each(|file_path| {
        if let Ok(sources) = extract_imports(file_path) {
            imports_map.insert(file_path.clone(), sources);

            let file_name = file_path.file_name().unwrap_or_default().to_string_lossy();
            if file_name.contains(&config.test_pattern) {
                test_files.insert(file_path.clone(), ());
            }
        }
    });

    // Phase 3: Resolve import specifiers to file paths
    let resolver = create_resolver(&root);

    let forward: DashMap<PathBuf, HashSet<PathBuf>> = DashMap::new();
    let reverse: DashMap<PathBuf, HashSet<PathBuf>> = DashMap::new();

    // Collect into a Vec for rayon par_iter
    let imports_vec: Vec<(PathBuf, Vec<String>)> = imports_map.into_iter().collect();

    imports_vec.par_iter().for_each(|(file_path, sources)| {
        let dir = file_path.parent().unwrap_or(&root);

        let mut resolved = HashSet::new();
        for source in sources {
            if let Some(resolved_path) = resolve_import(&resolver, dir, source) {
                resolved.insert(resolved_path.clone());

                reverse
                    .entry(resolved_path)
                    .or_insert_with(HashSet::new)
                    .insert(file_path.clone());
            }
        }

        forward.insert(file_path.clone(), resolved);
    });

    let forward: HashMap<PathBuf, HashSet<PathBuf>> = forward.into_iter().collect();
    let reverse: HashMap<PathBuf, HashSet<PathBuf>> = reverse.into_iter().collect();
    let test_files: Vec<PathBuf> = test_files.into_iter().map(|(k, _)| k).collect();

    Ok(ImportGraph {
        forward,
        reverse,
        test_files,
    })
}

/// Given changed files, find all affected test files via reverse dependency traversal.
pub fn find_affected_tests(
    graph: &ImportGraph,
    changed_files: &[PathBuf],
) -> Result<Vec<PathBuf>> {
    let test_set: HashSet<&PathBuf> = graph.test_files.iter().collect();
    let mut affected: HashSet<PathBuf> = HashSet::new();
    let mut visited: HashSet<PathBuf> = HashSet::new();
    let mut queue: VecDeque<PathBuf> = VecDeque::new();

    // Seed the BFS with changed files
    for file in changed_files {
        let canonical = if file.is_absolute() {
            file.clone()
        } else {
            file.canonicalize().unwrap_or_else(|_| file.clone())
        };
        queue.push_back(canonical);
    }

    // BFS through reverse edges
    while let Some(current) = queue.pop_front() {
        if !visited.insert(current.clone()) {
            continue;
        }

        // If this file is a test, mark it as affected
        if test_set.contains(&current) {
            affected.insert(current.clone());
        }

        // Traverse reverse edges (files that import this file)
        if let Some(dependents) = graph.reverse.get(&current) {
            for dep in dependents {
                if !visited.contains(dep) {
                    queue.push_back(dep.clone());
                }
            }
        }
    }

    let mut result: Vec<PathBuf> = affected.into_iter().collect();
    result.sort();
    Ok(result)
}

/// Discover all source files matching the given extensions.
/// Uses the `ignore` crate's parallel walker for multi-threaded traversal
/// with native .gitignore support (automatically skips node_modules, .git, etc.).
fn discover_source_files(
    root: &Path,
    extensions: &[String],
    ignore_dirs: &[String],
) -> Vec<PathBuf> {
    let ext_set: HashSet<&str> = extensions.iter().map(|s| s.as_str()).collect();

    let mut builder = WalkBuilder::new(root);
    builder.hidden(false).git_ignore(true).git_global(false);

    // Add custom ignore dirs as overrides
    let mut overrides = ignore::overrides::OverrideBuilder::new(root);
    for dir in ignore_dirs {
        // Ignore pattern: negate match to skip these directories
        let _ = overrides.add(&format!("!{}/**", dir));
    }
    if let Ok(ov) = overrides.build() {
        builder.overrides(ov);
    }

    let results: DashMap<PathBuf, ()> = DashMap::new();
    let ext_set_ref = &ext_set;
    let results_ref = &results;

    builder.build_parallel().run(|| {
        Box::new(move |entry| {
            let entry = match entry {
                Ok(e) => e,
                Err(_) => return ignore::WalkState::Continue,
            };

            if !entry.file_type().map(|ft| ft.is_file()).unwrap_or(false) {
                return ignore::WalkState::Continue;
            }

            let path = entry.path();
            if let Some(ext) = path.extension() {
                if ext_set_ref.contains(ext.to_string_lossy().as_ref()) {
                    results_ref.insert(path.to_path_buf(), ());
                }
            }

            ignore::WalkState::Continue
        })
    });

    results.into_iter().map(|(k, _)| k).collect()
}

/// Parse a TypeScript/JavaScript file and extract all import source strings.
fn extract_imports(file_path: &Path) -> Result<Vec<String>> {
    let source = std::fs::read_to_string(file_path)
        .with_context(|| format!("read {}", file_path.display()))?;

    let allocator = Allocator::default();
    let source_type = SourceType::from_path(file_path).unwrap_or_default();
    let parser = Parser::new(&allocator, &source, source_type);
    let result = parser.parse();

    // Extract imports by iterating over top-level statements directly
    let mut sources = Vec::new();
    for stmt in &result.program.body {
        match stmt {
            Statement::ImportDeclaration(decl) => {
                sources.push(decl.source.value.to_string());
            }
            Statement::ExportNamedDeclaration(decl) => {
                if let Some(ref src) = decl.source {
                    sources.push(src.value.to_string());
                }
            }
            Statement::ExportAllDeclaration(decl) => {
                sources.push(decl.source.value.to_string());
            }
            Statement::TSImportEqualsDeclaration(decl) => {
                if let oxc_ast::ast::TSModuleReference::ExternalModuleReference(ref ext) =
                    decl.module_reference
                {
                    sources.push(ext.expression.value.to_string());
                }
            }
            _ => {}
        }
    }

    // Also handle dynamic imports via simple string scanning (fast path)
    extract_dynamic_imports(&source, &mut sources);

    Ok(sources)
}

/// Extract dynamic import() calls via SIMD-accelerated substring search.
/// This catches `await import("./foo")` and `import("./bar")` patterns.
fn extract_dynamic_imports(source: &str, sources: &mut Vec<String>) {
    let bytes = source.as_bytes();
    let len = bytes.len();
    let finder = memmem::Finder::new(b"import(");

    for pos in finder.find_iter(bytes) {
        let start = pos + 7; // len("import(")
        // Skip whitespace
        let mut j = start;
        while j < len && bytes[j].is_ascii_whitespace() {
            j += 1;
        }
        // Check for quote character
        if j < len && (bytes[j] == b'"' || bytes[j] == b'\'' || bytes[j] == b'`') {
            let quote = bytes[j];
            let str_start = j + 1;
            let mut str_end = str_start;
            while str_end < len && bytes[str_end] != quote {
                str_end += 1;
            }
            if str_end < len {
                if let Ok(s) = std::str::from_utf8(&bytes[str_start..str_end]) {
                    // Only include relative/alias imports, not bare specifiers for npm packages
                    if s.starts_with('.') || s.starts_with('/') || s.contains('/') {
                        sources.push(s.to_string());
                    }
                }
            }
        }
    }
}

/// Create an oxc_resolver with TypeScript-aware settings.
fn create_resolver(root: &Path) -> Resolver {
    let mut extension_alias: Vec<(String, Vec<String>)> = Vec::new();
    // NodeNext moduleResolution: .js imports resolve to .ts files
    extension_alias.push((".js".into(), vec![".ts".into(), ".tsx".into(), ".js".into()]));
    extension_alias.push((".jsx".into(), vec![".tsx".into(), ".jsx".into()]));
    extension_alias.push((".mjs".into(), vec![".mts".into(), ".mjs".into()]));
    extension_alias.push((".cjs".into(), vec![".cts".into(), ".cjs".into()]));

    let options = ResolveOptions {
        extensions: vec![
            ".ts".into(),
            ".tsx".into(),
            ".js".into(),
            ".jsx".into(),
            ".mts".into(),
            ".mjs".into(),
            ".json".into(),
        ],
        extension_alias,
        main_fields: vec!["module".into(), "main".into()],
        condition_names: vec!["import".into(), "require".into(), "default".into()],
        tsconfig: Some(oxc_resolver::TsconfigOptions {
            config_file: root.join("tsconfig.json"),
            references: oxc_resolver::TsconfigReferences::Auto,
        }),
        ..Default::default()
    };
    Resolver::new(options)
}

/// Resolve an import specifier to a file path.
fn resolve_import(resolver: &Resolver, dir: &Path, specifier: &str) -> Option<PathBuf> {
    // Skip bare node builtins
    if specifier.starts_with("node:") || is_node_builtin(specifier) {
        return None;
    }

    match resolver.resolve(dir, specifier) {
        Ok(resolution) => Some(resolution.into_path_buf()),
        Err(_) => None,
    }
}

fn is_node_builtin(specifier: &str) -> bool {
    matches!(
        specifier.split('/').next().unwrap_or(specifier),
        "fs" | "path" | "os" | "util" | "crypto" | "http" | "https" | "net" | "stream"
        | "events" | "buffer" | "url" | "querystring" | "child_process" | "cluster"
        | "dgram" | "dns" | "readline" | "tls" | "zlib" | "assert" | "tty" | "v8"
        | "vm" | "worker_threads" | "perf_hooks" | "async_hooks" | "inspector"
        | "string_decoder" | "timers" | "console" | "module" | "process"
    )
}

/// Serializable output for affected test analysis.
#[derive(serde::Serialize)]
pub struct AnalysisResult {
    pub changed_files: Vec<String>,
    pub affected_tests: Vec<String>,
    pub total_tests: usize,
    pub graph_nodes: usize,
    pub graph_edges: usize,
    pub elapsed_ms: u64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::{HashMap, HashSet};
    use std::path::PathBuf;

    // ── extract_dynamic_imports ──────────────────────────────────────────────

    #[test]
    fn extract_dynamic_imports_double_quotes() {
        let mut sources = Vec::new();
        extract_dynamic_imports(r#"const m = import("./foo");"#, &mut sources);
        assert_eq!(sources, vec!["./foo"]);
    }

    #[test]
    fn extract_dynamic_imports_single_quotes() {
        let mut sources = Vec::new();
        extract_dynamic_imports("const m = import('./bar');", &mut sources);
        assert_eq!(sources, vec!["./bar"]);
    }

    #[test]
    fn extract_dynamic_imports_backtick() {
        let mut sources = Vec::new();
        extract_dynamic_imports("const m = import(`./baz`);", &mut sources);
        assert_eq!(sources, vec!["./baz"]);
    }

    #[test]
    fn extract_dynamic_imports_with_whitespace() {
        let mut sources = Vec::new();
        extract_dynamic_imports("await import(  \"./spaced\"  )", &mut sources);
        assert_eq!(sources, vec!["./spaced"]);
    }

    #[test]
    fn extract_dynamic_imports_skips_bare_specifiers() {
        let mut sources = Vec::new();
        extract_dynamic_imports(r#"import("lodash")"#, &mut sources);
        assert!(sources.is_empty(), "bare npm specifier should be skipped");
    }

    #[test]
    fn extract_dynamic_imports_includes_scoped_packages() {
        let mut sources = Vec::new();
        extract_dynamic_imports(r#"import("@scope/pkg")"#, &mut sources);
        assert_eq!(sources, vec!["@scope/pkg"], "scoped package has a slash so it's included");
    }

    #[test]
    fn extract_dynamic_imports_multiple() {
        let mut sources = Vec::new();
        let code = r#"
            import("./a");
            import('./b');
            import(`./c`);
        "#;
        extract_dynamic_imports(code, &mut sources);
        assert_eq!(sources, vec!["./a", "./b", "./c"]);
    }

    #[test]
    fn extract_dynamic_imports_empty_source() {
        let mut sources = Vec::new();
        extract_dynamic_imports("", &mut sources);
        assert!(sources.is_empty());
    }

    #[test]
    fn extract_dynamic_imports_absolute_path() {
        let mut sources = Vec::new();
        extract_dynamic_imports(r#"import("/absolute/path")"#, &mut sources);
        assert_eq!(sources, vec!["/absolute/path"]);
    }

    // ── is_node_builtin ─────────────────────────────────────────────────────

    #[test]
    fn node_builtins_detected() {
        for name in &["fs", "path", "os", "crypto", "http", "https", "child_process", "events"] {
            assert!(is_node_builtin(name), "{} should be detected as a builtin", name);
        }
    }

    #[test]
    fn node_builtin_subpath_detected() {
        assert!(is_node_builtin("fs/promises"), "fs/promises should match via split('/')");
    }

    #[test]
    fn non_builtins_rejected() {
        assert!(!is_node_builtin("express"));
        assert!(!is_node_builtin("./local"));
        assert!(!is_node_builtin("@scope/pkg"));
    }

    // ── AnalyzerConfig defaults ─────────────────────────────────────────────

    #[test]
    fn analyzer_config_defaults() {
        let cfg = AnalyzerConfig::default();
        assert_eq!(cfg.root, PathBuf::from("."));
        assert!(cfg.extensions.contains(&"ts".to_string()));
        assert!(cfg.extensions.contains(&"tsx".to_string()));
        assert!(cfg.extensions.contains(&"js".to_string()));
        assert!(cfg.ignore_dirs.contains(&"node_modules".to_string()));
        assert!(cfg.ignore_dirs.contains(&"dist".to_string()));
        assert_eq!(cfg.test_pattern, ".test.");
    }

    // ── discover_source_files ───────────────────────────────────────────────

    #[test]
    fn discover_source_files_finds_ts_files() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();

        std::fs::write(root.join("a.ts"), "export const a = 1;").unwrap();
        std::fs::write(root.join("b.js"), "export const b = 2;").unwrap();
        std::fs::write(root.join("c.txt"), "not source").unwrap();

        let files = discover_source_files(
            root,
            &["ts".into(), "js".into()],
            &["node_modules".into()],
        );
        assert_eq!(files.len(), 2);
        let names: HashSet<String> = files
            .iter()
            .map(|f| f.file_name().unwrap().to_string_lossy().to_string())
            .collect();
        assert!(names.contains("a.ts"));
        assert!(names.contains("b.js"));
    }

    #[test]
    fn discover_source_files_ignores_dirs() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();

        std::fs::create_dir(root.join("node_modules")).unwrap();
        std::fs::write(root.join("node_modules/dep.ts"), "").unwrap();
        std::fs::write(root.join("src.ts"), "").unwrap();

        let files = discover_source_files(root, &["ts".into()], &["node_modules".into()]);
        assert_eq!(files.len(), 1);
        assert!(files[0].ends_with("src.ts"));
    }

    // ── find_affected_tests ─────────────────────────────────────────────────

    fn make_graph(
        forward: Vec<(&str, Vec<&str>)>,
        reverse: Vec<(&str, Vec<&str>)>,
        tests: Vec<&str>,
    ) -> ImportGraph {
        let to_map = |pairs: Vec<(&str, Vec<&str>)>| -> HashMap<PathBuf, HashSet<PathBuf>> {
            pairs
                .into_iter()
                .map(|(k, vs)| {
                    (
                        PathBuf::from(k),
                        vs.into_iter().map(PathBuf::from).collect(),
                    )
                })
                .collect()
        };

        ImportGraph {
            forward: to_map(forward),
            reverse: to_map(reverse),
            test_files: tests.into_iter().map(PathBuf::from).collect(),
        }
    }

    #[test]
    fn find_affected_tests_direct_change() {
        // Changing a test file itself should mark it as affected
        let graph = make_graph(
            vec![("/a.test.ts", vec!["/a.ts"])],
            vec![("/a.ts", vec!["/a.test.ts"])],
            vec!["/a.test.ts"],
        );
        let affected = find_affected_tests(&graph, &[PathBuf::from("/a.test.ts")]).unwrap();
        assert_eq!(affected, vec![PathBuf::from("/a.test.ts")]);
    }

    #[test]
    fn find_affected_tests_transitive() {
        // a.test.ts -> b.ts -> c.ts; changing c.ts should affect a.test.ts
        let graph = make_graph(
            vec![
                ("/a.test.ts", vec!["/b.ts"]),
                ("/b.ts", vec!["/c.ts"]),
            ],
            vec![
                ("/c.ts", vec!["/b.ts"]),
                ("/b.ts", vec!["/a.test.ts"]),
            ],
            vec!["/a.test.ts"],
        );
        let affected = find_affected_tests(&graph, &[PathBuf::from("/c.ts")]).unwrap();
        assert_eq!(affected, vec![PathBuf::from("/a.test.ts")]);
    }

    #[test]
    fn find_affected_tests_no_match() {
        let graph = make_graph(
            vec![("/a.test.ts", vec!["/a.ts"])],
            vec![("/a.ts", vec!["/a.test.ts"])],
            vec!["/a.test.ts"],
        );
        // /unrelated.ts has no reverse edges, so no tests affected
        let affected = find_affected_tests(&graph, &[PathBuf::from("/unrelated.ts")]).unwrap();
        assert!(affected.is_empty());
    }

    #[test]
    fn find_affected_tests_multiple_tests() {
        // Both test1 and test2 depend on shared.ts
        let graph = make_graph(
            vec![
                ("/test1.test.ts", vec!["/shared.ts"]),
                ("/test2.test.ts", vec!["/shared.ts"]),
            ],
            vec![("/shared.ts", vec!["/test1.test.ts", "/test2.test.ts"])],
            vec!["/test1.test.ts", "/test2.test.ts"],
        );
        let affected = find_affected_tests(&graph, &[PathBuf::from("/shared.ts")]).unwrap();
        assert_eq!(affected.len(), 2);
    }

    #[test]
    fn find_affected_tests_handles_cycles() {
        // a -> b -> a (cycle); changing a should still terminate
        let graph = make_graph(
            vec![("/a.test.ts", vec!["/b.ts"]), ("/b.ts", vec!["/a.test.ts"])],
            vec![("/a.test.ts", vec!["/b.ts"]), ("/b.ts", vec!["/a.test.ts"])],
            vec!["/a.test.ts"],
        );
        let affected = find_affected_tests(&graph, &[PathBuf::from("/b.ts")]).unwrap();
        assert_eq!(affected, vec![PathBuf::from("/a.test.ts")]);
    }
}
