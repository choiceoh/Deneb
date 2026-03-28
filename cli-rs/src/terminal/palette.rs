use console::Style;

/// Refined palette tokens for CLI theming.
/// Inspired by Apple's restrained, elegant color language.
#[allow(dead_code)]
pub struct Palette;

#[allow(dead_code)]
impl Palette {
    pub fn accent() -> Style {
        Style::new().color256(173) // soft coral #D0876A
    }

    #[allow(dead_code)]
    pub fn accent_bright() -> Style {
        Style::new().color256(216) // soft peach #FFAF87
    }

    #[allow(dead_code)]
    pub fn accent_dim() -> Style {
        Style::new().color256(131) // dusty rose #AF5F5F
    }

    #[allow(dead_code)]
    pub fn info() -> Style {
        Style::new().color256(75) // soft blue #5FAFFF
    }

    pub fn success() -> Style {
        Style::new().color256(72) // sage green #5FAF87
    }

    pub fn warn() -> Style {
        Style::new().color256(179) // warm amber #D7AF5F
    }

    pub fn error() -> Style {
        Style::new().color256(167) // soft red #D75F5F
    }

    pub fn muted() -> Style {
        Style::new().color256(246) // light gray #949494
    }

    pub fn bold() -> Style {
        Style::new().bold()
    }

    #[allow(dead_code)]
    pub fn dim() -> Style {
        Style::new().dim()
    }

    /// Dim style for decorative separators and lines.
    #[allow(dead_code)]
    pub fn separator() -> Style {
        Style::new().dim()
    }
}
