use console::Style;

/// Lobster palette tokens for CLI theming.
/// Kept in sync with `src/terminal/palette.ts`.
#[allow(dead_code)]
pub struct Palette;

#[allow(dead_code)]
impl Palette {
    pub fn accent() -> Style {
        Style::new().color256(202) // #FF5A2D approx
    }

    pub fn accent_bright() -> Style {
        Style::new().color256(209) // #FF7A3D approx
    }

    pub fn accent_dim() -> Style {
        Style::new().color256(166) // #D14A22 approx
    }

    pub fn info() -> Style {
        Style::new().color256(209) // #FF8A5B approx
    }

    pub fn success() -> Style {
        Style::new().color256(35) // #2FBF71 approx
    }

    pub fn warn() -> Style {
        Style::new().color256(214) // #FFB020 approx
    }

    pub fn error() -> Style {
        Style::new().color256(160) // #E23D2D approx
    }

    pub fn muted() -> Style {
        Style::new().color256(245) // #8B7F77 approx
    }

    pub fn bold() -> Style {
        Style::new().bold()
    }

    pub fn dim() -> Style {
        Style::new().dim()
    }
}
