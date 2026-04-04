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

#[cfg(test)]
mod tests {
    use super::{is_json_mode, is_rich};

    #[test]
    fn is_json_mode_true_when_flag_enabled_even_without_env() {
        unsafe {
            std::env::remove_var("DENEB_OUTPUT_JSON");
        }
        assert!(is_json_mode(true));
    }

    #[test]
    fn is_json_mode_true_when_env_is_one() {
        unsafe {
            std::env::set_var("DENEB_OUTPUT_JSON", "1");
        }
        assert!(is_json_mode(false));
        unsafe {
            std::env::remove_var("DENEB_OUTPUT_JSON");
        }
    }

    #[test]
    fn is_json_mode_false_when_flag_off_and_env_not_one() {
        unsafe {
            std::env::set_var("DENEB_OUTPUT_JSON", "true");
        }
        assert!(!is_json_mode(false));
        unsafe {
            std::env::remove_var("DENEB_OUTPUT_JSON");
        }
    }

    #[test]
    fn is_rich_false_when_no_color_set() {
        unsafe {
            std::env::set_var("NO_COLOR", "1");
            std::env::remove_var("TERM");
        }
        assert!(!is_rich());
        unsafe {
            std::env::remove_var("NO_COLOR");
        }
    }

    #[test]
    fn is_rich_false_when_term_is_dumb() {
        unsafe {
            std::env::remove_var("NO_COLOR");
            std::env::set_var("TERM", "dumb");
        }
        assert!(!is_rich());
        unsafe {
            std::env::remove_var("TERM");
        }
    }
}
