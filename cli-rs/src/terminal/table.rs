use comfy_table::{presets, Table};

/// Create a styled table with light horizontal-only borders.
/// Minimal chrome — Apple-style airy layout without cage-like full borders.
pub fn styled_table() -> Table {
    let mut table = Table::new();
    table.load_preset(presets::UTF8_HORIZONTAL_ONLY);
    table.set_content_arrangement(comfy_table::ContentArrangement::Dynamic);
    table
}

/// Create a minimal borderless table (for compact output).
#[allow(dead_code)]
pub fn compact_table() -> Table {
    let mut table = Table::new();
    table.load_preset(presets::NOTHING);
    table.set_content_arrangement(comfy_table::ContentArrangement::Dynamic);
    table
}
