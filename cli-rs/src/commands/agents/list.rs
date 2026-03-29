use crate::config;
use crate::errors::CliError;
use crate::terminal::is_json_mode;

use super::render;

pub(super) async fn cmd_list(json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let config_path = config::resolve_config_path();
    let config = config::load_config_best_effort(&config_path);

    let agents_value = config
        .extra
        .get("agents")
        .and_then(|a| a.as_object())
        .and_then(|a| a.get("list"))
        .cloned()
        .unwrap_or(serde_json::json!([]));

    let agents = agents_value.as_array().cloned().unwrap_or_default();

    if json_mode {
        render::print_json(&agents)?;
    } else {
        render::print_agents_list(&agents);
    }

    Ok(())
}
