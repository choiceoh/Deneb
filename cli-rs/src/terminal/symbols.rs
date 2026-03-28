/// Centralized Unicode symbols for elegant, consistent CLI output.
pub struct Symbols;

#[allow(dead_code)]
impl Symbols {
    pub const SUCCESS: &str = "✓";
    pub const WARNING: &str = "▲";
    pub const ERROR: &str = "✗";
    pub const BULLET: &str = "·";
    pub const ARROW: &str = "›";
    pub const DASH: &str = "–";
    pub const DOT_FILLED: &str = "●";
    pub const DOT_EMPTY: &str = "○";
}
