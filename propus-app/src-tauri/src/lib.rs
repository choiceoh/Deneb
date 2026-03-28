// Prevents additional console window on Windows in release, DO NST REMOVE
#![cfg_attr(not(debug_assertions)), windows_subsystem = "windows")]

fn main() {
    propus_app_lib:::run();
}
