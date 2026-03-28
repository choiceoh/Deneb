use assert_cmd::Command;
use predicates::prelude::*;

fn deneb_cmd() -> Command {
    Command::cargo_bin("deneb-rs").unwrap()
}

// --- Help & version ---

#[test]
fn help_flag_shows_usage() {
    deneb_cmd()
        .arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("Deneb CLI"))
        .stdout(predicate::str::contains("USAGE").or(predicate::str::contains("Usage")));
}

#[test]
fn version_flag_shows_version() {
    deneb_cmd()
        .arg("--version")
        .assert()
        .success()
        .stdout(predicate::str::contains("deneb"));
}

// --- Config commands (local, no gateway) ---

#[test]
fn config_file_shows_path() {
    deneb_cmd()
        .args(["config", "file"])
        .assert()
        .success()
        .stdout(predicate::str::contains("deneb.json"));
}

#[test]
fn config_get_missing_key_exits_error() {
    // Getting a non-existent key returns exit code 1
    deneb_cmd()
        .args(["config", "get", "nonexistent.key.path"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("not found").or(predicate::str::contains("Key")));
}

#[test]
fn config_validate_works() {
    deneb_cmd().args(["config", "validate"]).assert().success();
}

// --- Setup (local, no gateway) ---

#[test]
fn setup_with_json_output() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["setup", "--json"])
        .assert()
        .success()
        .stdout(predicate::str::contains("configPath"))
        .stdout(predicate::str::contains("workspace"));
}

// --- Sessions list (local, no gateway) ---

#[test]
fn sessions_list_without_data() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["sessions", "--json"])
        .assert()
        .success();
}

// --- Agents list (local, no gateway) ---

#[test]
fn agents_list_empty() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["agents", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("No agents configured"));
}

// --- Health (fails without gateway, but should fail gracefully) ---

#[test]
fn health_fails_without_gateway() {
    deneb_cmd()
        .args(["health", "--timeout", "500"])
        .env("DENEB_STATE_DIR", "/tmp/deneb-test-nonexistent")
        .assert()
        .failure()
        .stderr(predicate::str::contains("\u{2717}")); // ✗ error symbol
}

// --- Dashboard no-open (local, no gateway) ---

#[test]
fn dashboard_no_open_shows_url() {
    deneb_cmd()
        .args(["dashboard", "--no-open"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Dashboard"));
}

// --- Completion (local) ---

#[test]
fn completion_bash() {
    deneb_cmd()
        .args(["completion", "--shell", "bash"])
        .assert()
        .success()
        .stdout(predicate::str::contains("deneb"));
}

#[test]
fn completion_zsh() {
    deneb_cmd()
        .args(["completion", "--shell", "zsh"])
        .assert()
        .success()
        .stdout(predicate::str::contains("deneb"));
}

// --- Security audit (local) ---

#[test]
fn security_audit_json() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["security", "--json"])
        .assert()
        .success()
        .stdout(predicate::str::starts_with("["));
}

// --- Backup list (local) ---

#[test]
fn backup_list_empty() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["backup", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("No backups found"));
}

// --- Doctor (fails gateway checks but succeeds overall) ---

#[test]
fn doctor_json_runs() {
    let dir = tempfile::tempdir().unwrap();
    deneb_cmd()
        .env("DENEB_STATE_DIR", dir.path())
        .args(["doctor", "--json", "--non-interactive", "--timeout", "500"])
        .assert()
        .success()
        .stdout(predicate::str::starts_with("["));
}

// --- Message send dry-run (no gateway needed) ---

#[test]
fn message_send_dry_run() {
    deneb_cmd()
        .args([
            "message",
            "send",
            "-t",
            "+1234567890",
            "-m",
            "test message",
            "--dry-run",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("+1234567890"))
        .stdout(predicate::str::contains("test message"))
        .stdout(predicate::str::contains("idempotencyKey"));
}

// --- Subcommand help ---

#[test]
fn gateway_help() {
    deneb_cmd()
        .args(["gateway", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("status"));
}

#[test]
fn plugins_help() {
    deneb_cmd()
        .args(["plugins", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("list"));
}

#[test]
fn cron_help() {
    deneb_cmd()
        .args(["cron", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("list"));
}
