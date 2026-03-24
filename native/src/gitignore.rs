use globset::{Glob, GlobSet, GlobSetBuilder};
use napi::bindgen_prelude::*;
use napi_derive::napi;

/// Metadata about a single parsed gitignore pattern.
#[napi(object)]
pub struct GitignorePatternInfo {
    pub raw: String,
    pub pattern: String,
    pub negated: bool,
    pub directory_only: bool,
}

/// Internal representation of a compiled pattern used for matching.
struct CompiledPattern {
    negated: bool,
    directory_only: bool,
    index: usize,
}

/// A compiled gitignore matcher that performs pattern matching entirely in Rust.
#[napi]
pub struct GitignoreMatcher {
    glob_set: GlobSet,
    patterns: Vec<CompiledPattern>,
    infos: Vec<GitignorePatternInfo>,
}

/// Parse gitignore content into (glob_pattern, negated, directory_only, raw, pattern) tuples.
/// This is the pure-Rust logic, testable without napi.
fn parse_gitignore_lines(content: &str) -> Vec<(String, bool, bool, String, String)> {
    let mut result = Vec::new();

    for line in content.lines() {
        let raw = line.to_string();
        let trimmed_end = line.trim_end();

        if trimmed_end.is_empty() || trimmed_end.starts_with('#') {
            continue;
        }

        let mut s = trimmed_end.to_string();

        if s.starts_with("\\#") {
            s = s[1..].to_string();
        }

        let negated;
        if s.starts_with("\\!") {
            s = s[1..].to_string();
            negated = false;
        } else if s.starts_with('!') {
            s = s[1..].to_string();
            negated = true;
        } else {
            negated = false;
        }

        let directory_only = s.ends_with('/');
        if directory_only {
            s.pop();
        }

        let anchored = s.starts_with('/');
        if anchored {
            s = s[1..].to_string();
        }

        if s.is_empty() {
            continue;
        }

        let has_slash = s.contains('/');
        let glob_pattern = if has_slash || anchored {
            s.clone()
        } else {
            format!("**/{s}")
        };

        result.push((glob_pattern, negated, directory_only, raw, s));
    }

    result
}

/// Pure-Rust matching logic: given glob_set matches and pattern metadata, determine ignored state.
fn compute_ignored(
    matches: &[usize],
    patterns: &[CompiledPattern],
    is_directory: bool,
) -> bool {
    let mut ignored = false;
    for &match_index in matches {
        if let Some(pat) = patterns.iter().find(|p| p.index == match_index) {
            if pat.directory_only && !is_directory {
                continue;
            }
            ignored = !pat.negated;
        }
    }
    ignored
}

#[napi]
impl GitignoreMatcher {
    #[napi(constructor)]
    pub fn new(content: String) -> Result<Self> {
        let mut builder = GlobSetBuilder::new();
        let mut patterns: Vec<CompiledPattern> = Vec::new();
        let mut infos: Vec<GitignorePatternInfo> = Vec::new();
        let mut index: usize = 0;

        for (glob_pattern, negated, directory_only, raw, pattern) in
            parse_gitignore_lines(&content)
        {
            match Glob::new(&glob_pattern) {
                Ok(glob) => {
                    builder.add(glob);
                    patterns.push(CompiledPattern {
                        negated,
                        directory_only,
                        index,
                    });
                    infos.push(GitignorePatternInfo {
                        raw,
                        pattern,
                        negated,
                        directory_only,
                    });
                    index += 1;
                }
                Err(_) => continue,
            }
        }

        let glob_set = builder
            .build()
            .map_err(|e| Error::from_reason(format!("Failed to build glob set: {e}")))?;

        Ok(GitignoreMatcher {
            glob_set,
            patterns,
            infos,
        })
    }

    #[napi]
    pub fn is_ignored(&self, file_path: String, is_directory: bool) -> bool {
        let normalized = file_path
            .replace('\\', "/")
            .trim_start_matches('/')
            .to_string();
        if normalized.is_empty() {
            return false;
        }

        let matches = self.glob_set.matches(&normalized);
        compute_ignored(&matches, &self.patterns, is_directory)
    }

    #[napi]
    pub fn get_patterns(&self) -> Vec<GitignorePatternInfo> {
        self.infos
            .iter()
            .map(|info| GitignorePatternInfo {
                raw: info.raw.clone(),
                pattern: info.pattern.clone(),
                negated: info.negated,
                directory_only: info.directory_only,
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Test helper: build a GlobSet and patterns from content, then check ignored state.
    fn is_ignored_pure(content: &str, path: &str, is_directory: bool) -> bool {
        let mut builder = GlobSetBuilder::new();
        let mut patterns = Vec::new();
        let mut index = 0usize;

        for (glob_pattern, negated, directory_only, _, _) in parse_gitignore_lines(content) {
            if let Ok(glob) = Glob::new(&glob_pattern) {
                builder.add(glob);
                patterns.push(CompiledPattern {
                    negated,
                    directory_only,
                    index,
                });
                index += 1;
            }
        }

        let glob_set = builder.build().unwrap();
        let normalized = path
            .replace('\\', "/")
            .trim_start_matches('/')
            .to_string();
        let matches = glob_set.matches(&normalized);
        compute_ignored(&matches, &patterns, is_directory)
    }

    fn count_patterns(content: &str) -> usize {
        parse_gitignore_lines(content).len()
    }

    #[test]
    fn test_basic_ignore() {
        let content = "*.log\nnode_modules/\n";
        assert!(is_ignored_pure(content, "debug.log", false));
        assert!(is_ignored_pure(content, "src/debug.log", false));
        assert!(is_ignored_pure(content, "node_modules", true));
        assert!(!is_ignored_pure(content, "node_modules", false));
    }

    #[test]
    fn test_negation() {
        let content = "*.log\n!important.log\n";
        assert!(is_ignored_pure(content, "debug.log", false));
        assert!(!is_ignored_pure(content, "important.log", false));
    }

    #[test]
    fn test_comments_and_blank_lines() {
        assert_eq!(count_patterns("# comment\n\n*.tmp\n"), 1);
        assert!(is_ignored_pure("# comment\n\n*.tmp\n", "file.tmp", false));
    }

    #[test]
    fn test_anchored_pattern() {
        let content = "/build\n";
        assert!(is_ignored_pure(content, "build", false));
        assert!(!is_ignored_pure(content, "src/build", false));
    }

    #[test]
    fn test_double_star() {
        let content = "**/logs\n";
        assert!(is_ignored_pure(content, "logs", false));
        assert!(is_ignored_pure(content, "src/logs", false));
        assert!(is_ignored_pure(content, "a/b/logs", false));
    }

    #[test]
    fn test_empty_content() {
        assert!(!is_ignored_pure("", "anything", false));
        assert_eq!(count_patterns(""), 0);
    }

    #[test]
    fn test_escaped_hash_and_bang() {
        let content = "\\#file\n\\!file\n";
        assert_eq!(count_patterns(content), 2);
        assert!(is_ignored_pure(content, "#file", false));
        assert!(is_ignored_pure(content, "!file", false));
    }
}
