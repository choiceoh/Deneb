use indicatif::{ProgressBar, ProgressStyle};

/// Create a spinner with a label message.
pub fn spinner(message: &str) -> ProgressBar {
    let pb = ProgressBar::new_spinner();
    pb.set_style(
        ProgressStyle::default_spinner()
            .tick_strings(&["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"])
            .template("{spinner:.cyan} {msg}")
            .expect("valid template"),
    );
    pb.set_message(message.to_string());
    pb.enable_steady_tick(std::time::Duration::from_millis(80));
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
