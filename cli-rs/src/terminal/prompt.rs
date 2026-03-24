use dialoguer::Confirm;

/// Ask a yes/no confirmation question.
pub fn confirm(message: &str, default: bool) -> Result<bool, dialoguer::Error> {
    Confirm::new()
        .with_prompt(message)
        .default(default)
        .interact()
}
