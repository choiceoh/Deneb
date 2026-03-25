use comfy_table::{presets, Table};

/// Create a styled table with the Deneb default style.
pub fn styled_table() -> Table {
    let mut table = Table::new();
    table.load_preset(presets::UTF8_FULL_CONDENSED);
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
