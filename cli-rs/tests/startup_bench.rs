use std::time::Instant;

use assert_cmd::Command;

/// Verify the CLI starts in under 50ms (target: <10ms).
/// This test measures cold-start time for `--version` which exercises
/// argument parsing but skips async runtime and gateway connections.
#[test]
fn startup_time_under_50ms() {
    // Warm up: first run may be slower due to dynamic linker
    let _ = Command::cargo_bin("deneb-rs")
        .unwrap()
        .arg("--version")
        .output();

    // Measure 5 runs and take the median
    let mut times = Vec::new();
    for _ in 0..5 {
        let start = Instant::now();
        let output = Command::cargo_bin("deneb-rs")
            .unwrap()
            .arg("--version")
            .output()
            .expect("failed to run deneb-rs");
        let elapsed = start.elapsed();
        assert!(output.status.success());
        times.push(elapsed.as_millis());
    }

    times.sort();
    let median = times[times.len() / 2];

    eprintln!("Startup times (ms): {times:?}");
    eprintln!("Median startup time: {median}ms");

    // Assert median is under 50ms (generous budget; target is <10ms)
    assert!(
        median < 50,
        "Startup time too slow: {median}ms (target: <50ms, goal: <10ms)"
    );
}
