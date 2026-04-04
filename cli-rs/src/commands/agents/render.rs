use crate::errors::CliError;
use crate::terminal::{Palette, Symbols};

pub(super) fn print_json<T: serde::Serialize>(value: &T) -> Result<(), CliError> {
    println!("{}", serde_json::to_string_pretty(value)?);
    Ok(())
}

pub(super) fn print_agents_list(agents: &[serde_json::Value]) {
    let bold = Palette::bold();
    let muted = Palette::muted();

    if agents.is_empty() {
        println!("    {}", muted.apply_to("No agents configured."));
        return;
    }

    println!();
    println!(
        "  {}  {}  {}",
        bold.apply_to("Agents"),
        muted.apply_to(Symbols::ARROW),
        muted.apply_to(format!("{} configured", agents.len()))
    );
    println!();

    for agent in agents {
        let id = agent
            .get("id")
            .and_then(|v| v.as_str())
            .unwrap_or("unknown");
        let name = agent.get("name").and_then(|v| v.as_str());
        let model = agent.get("model").and_then(|v| v.as_str());
        let is_default = agent
            .get("isDefault")
            .and_then(serde_json::Value::as_bool)
            .unwrap_or(false);

        let accent = Palette::accent();
        let label = if is_default {
            format!("{id} (default)")
        } else {
            id.to_string()
        };
        println!(
            "    {} {}",
            muted.apply_to(Symbols::ARROW),
            accent.apply_to(&label)
        );

        if let Some(name) = name {
            println!("      Name     {}", muted.apply_to(name));
        }
        if let Some(model) = model {
            println!("      Model    {}", muted.apply_to(model));
        }
        println!();
    }
}

pub(super) fn print_success(message: &str) {
    let success = Palette::success();
    println!(
        "    {}  {}",
        success.apply_to(Symbols::SUCCESS),
        success.apply_to(message)
    );
}
