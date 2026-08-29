use keyring::Entry;
use std::fs;

// Computer use (the gateway's `computer` tool): screenshot + mouse/keyboard on
// the host OS. Desktop only — the frontend gates it behind an explicit setting.
#[cfg(desktop)]
mod computer;
#[cfg(desktop)]
use computer::computer_action;
#[cfg(not(desktop))]
#[tauri::command]
async fn computer_action(_cmd: serde_json::Value) -> Result<serde_json::Value, String> {
    Err("computer use is desktop-only".into())
}

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

// Dock/taskbar badge for pending proactive nudges (0 clears it). Frontend drives
// the count from the ProactivePanel list.
#[tauri::command]
fn set_badge(window: tauri::WebviewWindow, count: u32) -> Result<(), String> {
    let n = if count == 0 { None } else { Some(count as i64) };
    window.set_badge_count(n).map_err(|e| e.to_string())
}

// Tray + focus helper: the resident-assistant pattern — closing the window hides
// it (SSE + notifications stay alive); the tray reopens or truly quits.
#[cfg(desktop)]
fn show_main_window(app: &tauri::AppHandle) {
    use tauri::Manager;
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
}

// Cygnus — the summonable agent companion window (tray menu + global shortcut).
// Same Vite bundle as the workstation; the init script flags which UI mounts
// (src/cygnus/windowKind.ts). Created lazily on first summon; afterwards the
// shortcut toggles hide/show, and window-close hides like the main window.
#[cfg(desktop)]
fn toggle_cygnus_window(app: &tauri::AppHandle) {
    use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};
    if let Some(w) = app.get_webview_window("cygnus") {
        let visible = w.is_visible().unwrap_or(false);
        let focused = w.is_focused().unwrap_or(false);
        if visible && focused {
            let _ = w.hide();
        } else {
            let _ = w.show();
            let _ = w.unminimize();
            let _ = w.set_focus();
        }
        return;
    }
    // Identity is triple-carried: the window LABEL (canonical — main.tsx asks
    // the Tauri API), the URL query (works in every webview), and the init
    // script. The Xvfb real-shell run showed a single carrier is fragile in
    // the webkit shell (the companion failed to take the cygnus branch until
    // query+label were added) — never rely on one signal alone.
    let built = WebviewWindowBuilder::new(app, "cygnus", WebviewUrl::App("index.html?window=cygnus".into()))
        .initialization_script("window.__CYGNUS__ = true;")
        .title("Cygnus")
        // Wide enough that the thread rail docks beside the conversation
        // (the 560px breakpoint in cygnus.css) instead of covering it — the
        // list is meant to stay up. Narrower is still supported: below the
        // breakpoint the rail falls back to an overlay drawer.
        .inner_size(720.0, 700.0)
        .min_inner_size(380.0, 520.0)
        .decorations(false)
        .transparent(true)
        // Match the main window: let the webview own drag-drop (HTML5 file drop).
        .disable_drag_drop_handler()
        .build();
    match built {
        Ok(w) => {
            let _ = w.set_focus();
        }
        Err(e) => eprintln!("cygnus window create failed: {e}"),
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();

    // Second launch focuses the existing window instead of racing a duplicate
    // SSE subscription + sync cursor. Must register before other plugins.
    #[cfg(desktop)]
    let builder = builder.plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
        show_main_window(app);
    }));

    // Restore last window size/position/monitor on launch.
    #[cfg(desktop)]
    let builder = builder.plugin(tauri_plugin_window_state::Builder::default().build());

    builder
        .plugin(tauri_plugin_process::init())
        // Native HTTP (reqwest) so gateway requests bypass the webview's CORS and
        // macOS WKWebView ATS (which blocks plain-HTTP gateways). See src/gateway.ts.
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_notification::init())
        .setup(|app| {
            // Updater is desktop-only; the frontend drives the check (see src/updater.ts).
            #[cfg(desktop)]
            {
                app.handle()
                    .plugin(tauri_plugin_updater::Builder::new().build())?;

                // System tray: left click reopens, menu offers 열기/종료. 종료 is
                // the only true exit — window close just hides (see below).
                use tauri::menu::{Menu, MenuItem};
                use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
                let show = MenuItem::with_id(app, "show", "열기", true, None::<&str>)?;
                let cygnus = MenuItem::with_id(app, "cygnus", "Cygnus", true, None::<&str>)?;
                let quit = MenuItem::with_id(app, "quit", "종료", true, None::<&str>)?;
                let menu = Menu::with_items(app, &[&show, &cygnus, &quit])?;
                let mut tray = TrayIconBuilder::with_id("andromeda-tray")
                    .menu(&menu)
                    .show_menu_on_left_click(false)
                    .tooltip("Andromeda")
                    .on_menu_event(|app, event| match event.id.as_ref() {
                        "show" => show_main_window(app),
                        "cygnus" => toggle_cygnus_window(app),
                        "quit" => app.exit(0),
                        _ => {}
                    })
                    .on_tray_icon_event(|tray, event| {
                        if let TrayIconEvent::Click {
                            button: MouseButton::Left,
                            button_state: MouseButtonState::Up,
                            ..
                        } = event
                        {
                            show_main_window(tray.app_handle());
                        }
                    });
                if let Some(icon) = app.default_window_icon() {
                    tray = tray.icon(icon.clone());
                }
                tray.build(app)?;

                // Global summon for the Cygnus companion — works while the app
                // is in the tray. Registration is best-effort: another app
                // holding the chord must not break startup.
                use tauri_plugin_global_shortcut::ShortcutState;
                const CYGNUS_SUMMON: &str = "CmdOrCtrl+Shift+Space";
                app.handle().plugin(
                    tauri_plugin_global_shortcut::Builder::new()
                        .with_handler(|app, _shortcut, event| {
                            if event.state == ShortcutState::Pressed {
                                toggle_cygnus_window(app);
                            }
                        })
                        .build(),
                )?;
                use tauri_plugin_global_shortcut::GlobalShortcutExt;
                if let Err(e) = app.handle().global_shortcut().register(CYGNUS_SUMMON) {
                    eprintln!("cygnus summon shortcut unavailable ({CYGNUS_SUMMON}): {e}");
                }
            }
            #[cfg(not(desktop))]
            let _ = app;
            Ok(())
        })
        .on_window_event(|_window, _event| {
            // Close-to-tray: keep the events SSE + catch-up sync alive so the
            // proactive channel doesn't die with the window. 종료 lives in the tray.
            #[cfg(desktop)]
            if let tauri::WindowEvent::CloseRequested { api, .. } = _event {
                let _ = _window.hide();
                api.prevent_close();
            }
        })
        .invoke_handler(tauri::generate_handler![
            token_set,
            token_get,
            token_from_file,
            set_badge,
            computer_action
        ])
        .run(tauri::generate_context!())
        .expect("error while running Andromeda");
}
