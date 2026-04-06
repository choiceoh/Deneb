/// Check if the terminal supports rich output (colors, unicode).
#[allow(dead_code)]
pub fn is_rich() -> bool {
    // Respect NO_COLOR convention (https://no-color.org/)
    if std::env::var("NO_COLOR").is_ok() {
        return false;
    }

    // Check TERM
    if let Ok(term) = std::env::var("TERM") {
        if term == "dumb" {
            return false;
        }
    }

    // Check if stdout is a terminal
    console::Term::stdout().features().colors_supported()
}

/// Check if JSON output mode is requested (--json flag or `DENEB_OUTPUT_JSON` env).
pub fn is_json_mode(json_flag: bool) -> bool {
    json_flag || std::env::var("DENEB_OUTPUT_JSON").ok().as_deref() == Some("1")
}
