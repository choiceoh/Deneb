use indicatif::{ProgressBar, ProgressStyle};

/// Create a spinner with a label message.
/// Uses minimal dot-rotation glyphs for a clean, geometric feel.
pub fn spinner(message: &str) -> ProgressBar {
    let pb = ProgressBar::new_spinner();
    pb.set_style(
        ProgressStyle::default_spinner()
            .tick_strings(&["◐", "◓", "◑", "◒"])
            .template("{spinner:.white} {msg}")
            .unwrap_or_else(|_| unreachable!("valid spinner template")),
    );
    pb.set_message(message.to_string());
    pb.enable_steady_tick(std::time::Duration::from_millis(100));
    pb
}

/// Run an async operation with a spinner, hiding it on completion.
pub async fn with_spinner<F, T>(message: &str, enabled: bool, f: F) -> T
where
    F: std::future::Future<Output = T>,
{
    if !enabled {
        return f.await;
    }
    let pb = spinner(message);
    let result = f.await;
    pb.finish_and_clear();
    result
}
