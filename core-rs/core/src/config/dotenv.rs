//! Dotenv file loading.
//!
//! 1:1 port of `gateway-go/internal/config/dotenv.go`.
//!
//! `load_dotenv_files` loads `.env` files into process environment variables.
//! Precedence (highest -> lowest): process env > ./.env > ~/.deneb/.env.
//! Existing non-empty env vars are never overridden.

use std::collections::HashMap;
use std::env;
use std::fs::File;
use std::io::{self, BufRead, BufReader};
use std::path::Path;

use crate::config::paths::resolve_state_dir;

/// Load `.env` files into `std::env`, respecting precedence.
///
/// Files are loaded in order: CWD `./.env` first, then state dir `~/.deneb/.env`.
/// Keys already present in the process environment are never overridden.
///
/// The `log_fn` callback receives log messages for diagnostics.
pub fn load_dotenv_files(log_fn: &dyn Fn(&str)) {
    let candidates = [
        ".env".to_string(),
        resolve_state_dir().join(".env").to_string_lossy().to_string(),
    ];

    for path in &candidates {
        match parse_dotenv(Path::new(path)) {
            Ok(pairs) => {
                let mut applied = 0;
                for (key, val) in &pairs {
                    if env::var(key).unwrap_or_default().is_empty() {
                        env::set_var(key, val);
                        applied += 1;
                    }
                }
                log_fn(&format!(
                    "loaded .env file path={path} keys={} applied={applied}",
                    pairs.len()
                ));
            }
            Err(e) => {
                if e.kind() != io::ErrorKind::NotFound {
                    log_fn(&format!("failed to read .env file path={path} error={e}"));
                }
            }
        }
    }
}

/// Parse a `.env` file and return key-value pairs.
///
/// Supports: blank lines, `#` comments, `KEY=VALUE`, `KEY="VALUE"`, `KEY='VALUE'`,
/// optional `export ` prefix. Does not support multi-line values or interpolation.
pub fn parse_dotenv(path: &Path) -> io::Result<HashMap<String, String>> {
    let file = File::open(path)?;
    let reader = BufReader::new(file);
    let mut pairs = HashMap::new();

    for line in reader.lines() {
        let line = line?;
        let line = line.trim().to_string();

        if line.is_empty() || line.starts_with('#') {
            continue;
        }

        // Strip optional "export " prefix.
        let line = line
            .strip_prefix("export ")
            .map(ToString::to_string)
            .unwrap_or(line);

        let Some(idx) = line.find('=') else {
            continue;
        };
        if idx == 0 {
            continue;
        }

        let key = line[..idx].trim().to_string();
        let mut val = line[idx + 1..].trim().to_string();

        // Strip matching outer quotes.
        if val.len() >= 2 {
            let first = val.as_bytes()[0];
            let last = val.as_bytes()[val.len() - 1];
            if (first == b'"' && last == b'"') || (first == b'\'' && last == b'\'') {
                val = val[1..val.len() - 1].to_string();
            }
        }

        pairs.insert(key, val);
    }

    Ok(pairs)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn parse_basic_dotenv() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join(".env");
        fs::write(
            &path,
            "# Comment\nKEY1=value1\nKEY2=\"quoted value\"\nKEY3='single quoted'\nexport KEY4=exported\n\nKEY5=\n",
        )
        .expect("write");

        let pairs = parse_dotenv(&path).expect("parse");
        assert_eq!(pairs.get("KEY1").map(String::as_str), Some("value1"));
        assert_eq!(
            pairs.get("KEY2").map(String::as_str),
            Some("quoted value")
        );
        assert_eq!(
            pairs.get("KEY3").map(String::as_str),
            Some("single quoted")
        );
        assert_eq!(pairs.get("KEY4").map(String::as_str), Some("exported"));
        assert_eq!(pairs.get("KEY5").map(String::as_str), Some(""));
    }

    #[test]
    fn parse_missing_file() {
        let result = parse_dotenv(Path::new("/tmp/nonexistent-dotenv-test"));
        assert!(result.is_err());
        assert_eq!(result.err().map(|e| e.kind()), Some(io::ErrorKind::NotFound));
    }

    #[test]
    fn parse_ignores_malformed_lines() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join(".env");
        fs::write(&path, "GOOD=value\n=bad\nnoequalssign\nALSO_GOOD=123\n").expect("write");

        let pairs = parse_dotenv(&path).expect("parse");
        assert_eq!(pairs.len(), 2);
        assert_eq!(pairs.get("GOOD").map(String::as_str), Some("value"));
        assert_eq!(pairs.get("ALSO_GOOD").map(String::as_str), Some("123"));
    }
}
