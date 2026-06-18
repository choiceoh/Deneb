use keyring::Entry;
use std::fs;

// Service namespace for keychain entries (one token per account, e.g. session key).
const SERVICE: &str = "ai.deneb.andromeda";

// Read the canonical client token the gateway writes to ~/.deneb/client_token, so
// the desktop app auto-connects without the user pasting it. Missing file → None.
#[tauri::command]
fn token_from_file() -> Result<Option<String>, String> {
    let home = dirs::home_dir().ok_or("no home directory")?;
    let path = home.join(".deneb").join("client_token");
    match fs::read_to_string(&path) {
        Ok(s) => Ok(Some(s.trim().to_string())),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e.to_string()),
    }
}

// Persist the Deneb client token in the OS keychain (macOS Keychain, Windows
// Credential Manager, Linux libsecret) rather than localStorage — DESIGN §6 calls
// for secure-store on the desktop shell.
#[tauri::command]
fn token_set(account: String, token: String) -> Result<(), String> {
    let entry = Entry::new(SERVICE, &account).map_err(|e| e.to_string())?;
    entry.set_password(&token).map_err(|e| e.to_string())
}

#[tauri::command]
fn token_get(account: String) -> Result<Option<String>, String> {
    let entry = Entry::new(SERVICE, &account).map_err(|e| e.to_string())?;
    match entry.get_password() {
        Ok(token) => Ok(Some(token)),
        Err(keyring::Error::NoEntry) => Ok(None),
        Err(e) => Err(e.to_string()),
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![token_set, token_get, token_from_file])
        .run(tauri::generate_context!())
        .expect("error while running Andromeda");
}
