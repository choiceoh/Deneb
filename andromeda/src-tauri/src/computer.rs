// Computer use executor — the host-OS half of the `computer` chat tool.
//
// The frontend (src/computer.ts) forwards a validated command; this module
// captures the screen, drives mouse/keyboard (enigo) and returns a serializable
// outcome. Design points:
//
// - Coordinates the model sends are in the pixel space of the LAST screenshot
//   it saw (possibly a zoomed region, possibly downscaled). We remember that
//   frame (origin in logical screen px + logical-px-per-image-px scale) and map
//   every pointer action through it, so the model never deals with HiDPI or
//   downscale factors.
// - Screenshots use what the platform already has (macOS `screencapture`,
//   X11 GetImage through x11rb, Wayland/desktop CLIs, Windows PowerShell) —
//   no pipewire/portal build dependencies. Captures are downscaled to
//   MAX_IMAGE_WIDTH so the upload + vision read stay cheap.
// - Everything runs on a blocking thread (enigo and capture are sync).
use base64::Engine;
use enigo::{Axis, Button, Coordinate, Direction, Enigo, Key, Keyboard, Mouse, Settings};
use image::{imageops::FilterType, RgbaImage};
use serde::{Deserialize, Serialize};
use std::io::Cursor;
use std::sync::Mutex;
use std::time::Duration;

const MAX_IMAGE_WIDTH: u32 = 1600;

#[derive(Debug, Deserialize, Default)]
pub struct ComputerCmd {
    pub action: String,
    pub x: Option<i32>,
    pub y: Option<i32>,
    pub to_x: Option<i32>,
    pub to_y: Option<i32>,
    pub width: Option<i32>,
    pub height: Option<i32>,
    pub text: Option<String>,
    pub key: Option<String>,
    pub button: Option<String>,
    pub scroll: Option<String>,
    pub amount: Option<i32>,
    pub wait_ms: Option<i32>,
}

#[derive(Debug, Serialize, Default)]
pub struct ComputerOutcome {
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub image: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub width: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub height: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
}

impl ComputerOutcome {
    fn fail(msg: impl Into<String>) -> Self {
        ComputerOutcome { ok: false, error: Some(msg.into()), ..Default::default() }
    }
    fn done() -> Self {
        ComputerOutcome { ok: true, ..Default::default() }
    }
}

// The last screenshot's coordinate frame: image (x,y) → logical screen
// (origin_x + x*scale, origin_y + y*scale). Identity until the first capture.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Frame {
    pub origin_x: f64,
    pub origin_y: f64,
    pub scale: f64,
}

impl Default for Frame {
    fn default() -> Self {
        Frame { origin_x: 0.0, origin_y: 0.0, scale: 1.0 }
    }
}

static LAST_FRAME: Mutex<Frame> = Mutex::new(Frame { origin_x: 0.0, origin_y: 0.0, scale: 1.0 });

fn last_frame() -> Frame {
    *LAST_FRAME.lock().unwrap_or_else(|e| e.into_inner())
}

fn set_last_frame(f: Frame) {
    *LAST_FRAME.lock().unwrap_or_else(|e| e.into_inner()) = f;
}

// Image-space → logical screen px (rounded).
pub fn map_to_screen(f: Frame, x: i32, y: i32) -> (i32, i32) {
    (
        (f.origin_x + x as f64 * f.scale).round() as i32,
        (f.origin_y + y as f64 * f.scale).round() as i32,
    )
}

// Logical screen px → image space of the last frame.
pub fn map_to_image(f: Frame, sx: i32, sy: i32) -> (i32, i32) {
    let s = if f.scale > 0.0 { f.scale } else { 1.0 };
    (
        ((sx as f64 - f.origin_x) / s).round() as i32,
        ((sy as f64 - f.origin_y) / s).round() as i32,
    )
}

// Downscale factor so width ≤ max (never upscales).
pub fn fit_scale(width: u32, max_width: u32) -> f64 {
    if width == 0 || width <= max_width {
        1.0
    } else {
        max_width as f64 / width as f64
    }
}

#[tauri::command]
pub async fn computer_action(cmd: ComputerCmd) -> Result<ComputerOutcome, String> {
    tauri::async_runtime::spawn_blocking(move || run(cmd))
        .await
        .map_err(|e| e.to_string())
}

fn run(cmd: ComputerCmd) -> ComputerOutcome {
    match cmd.action.as_str() {
        "screenshot" => screenshot(&cmd),
        "wait" => {
            let ms = cmd.wait_ms.unwrap_or(1000).clamp(1, 10_000) as u64;
            std::thread::sleep(Duration::from_millis(ms));
            ComputerOutcome::done()
        }
        "click" | "double_click" | "right_click" | "move" | "drag" | "scroll" | "type" | "key" | "cursor" => {
            let mut enigo = match Enigo::new(&Settings::default()) {
                Ok(e) => e,
                Err(e) => return ComputerOutcome::fail(format!("입력 장치 초기화 실패: {e}")),
            };
            match input_action(&mut enigo, &cmd) {
                Ok(out) => out,
                Err(e) => ComputerOutcome::fail(e),
            }
        }
        other => ComputerOutcome::fail(format!("지원하지 않는 동작: {other}")),
    }
}

fn need_xy(cmd: &ComputerCmd) -> Result<(i32, i32), String> {
    match (cmd.x, cmd.y) {
        (Some(x), Some(y)) => Ok(map_to_screen(last_frame(), x, y)),
        _ => Err("좌표(x,y)가 필요합니다".into()),
    }
}

fn input_action(enigo: &mut Enigo, cmd: &ComputerCmd) -> Result<ComputerOutcome, String> {
    let e = |err: enigo::InputError| format!("입력 실패: {err}");
    match cmd.action.as_str() {
        "move" => {
            let (x, y) = need_xy(cmd)?;
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
        }
        "click" => {
            let (x, y) = need_xy(cmd)?;
            let button = match cmd.button.as_deref().unwrap_or("left") {
                "right" => Button::Right,
                "middle" => Button::Middle,
                _ => Button::Left,
            };
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
            settle();
            enigo.button(button, Direction::Click).map_err(e)?;
        }
        "double_click" => {
            let (x, y) = need_xy(cmd)?;
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
            settle();
            enigo.button(Button::Left, Direction::Click).map_err(e)?;
            std::thread::sleep(Duration::from_millis(60));
            enigo.button(Button::Left, Direction::Click).map_err(e)?;
        }
        "right_click" => {
            let (x, y) = need_xy(cmd)?;
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
            settle();
            enigo.button(Button::Right, Direction::Click).map_err(e)?;
        }
        "drag" => {
            let (x, y) = need_xy(cmd)?;
            let (tx, ty) = match (cmd.to_x, cmd.to_y) {
                (Some(a), Some(b)) => map_to_screen(last_frame(), a, b),
                _ => return Err("끝점(to_x,to_y)이 필요합니다".into()),
            };
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
            settle();
            enigo.button(Button::Left, Direction::Press).map_err(e)?;
            // A midpoint step makes drag-and-drop targets register a real move.
            std::thread::sleep(Duration::from_millis(80));
            enigo.move_mouse((x + tx) / 2, (y + ty) / 2, Coordinate::Abs).map_err(e)?;
            std::thread::sleep(Duration::from_millis(80));
            enigo.move_mouse(tx, ty, Coordinate::Abs).map_err(e)?;
            std::thread::sleep(Duration::from_millis(80));
            enigo.button(Button::Left, Direction::Release).map_err(e)?;
        }
        "scroll" => {
            let (x, y) = need_xy(cmd)?;
            enigo.move_mouse(x, y, Coordinate::Abs).map_err(e)?;
            settle();
            let amount = cmd.amount.unwrap_or(3).clamp(1, 50);
            let (len, axis) = match cmd.scroll.as_deref().unwrap_or("down") {
                "up" => (-amount, Axis::Vertical),
                "left" => (-amount, Axis::Horizontal),
                "right" => (amount, Axis::Horizontal),
                _ => (amount, Axis::Vertical),
            };
            enigo.scroll(len, axis).map_err(e)?;
        }
        "type" => {
            let text = cmd.text.as_deref().unwrap_or("");
            if text.is_empty() {
                return Err("입력할 텍스트가 없습니다".into());
            }
            enigo.text(text).map_err(e)?;
        }
        "key" => {
            let combo = cmd.key.as_deref().unwrap_or("").trim();
            let (mods, key) = parse_key_combo(combo)?;
            for m in &mods {
                enigo.key(*m, Direction::Press).map_err(e)?;
            }
            let res = enigo.key(key, Direction::Click).map_err(e);
            for m in mods.iter().rev() {
                let _ = enigo.key(*m, Direction::Release);
            }
            res?;
        }
        "cursor" => {
            let (sx, sy) = enigo.location().map_err(e)?;
            let (ix, iy) = map_to_image(last_frame(), sx, sy);
            return Ok(ComputerOutcome {
                ok: true,
                text: Some(format!("커서 위치: ({ix},{iy}) — 직전 스크린샷 좌표계 (화면 논리 좌표 ({sx},{sy}))")),
                ..Default::default()
            });
        }
        other => return Err(format!("지원하지 않는 동작: {other}")),
    }
    Ok(ComputerOutcome::done())
}

// Small pause between pointer move and button so the target window sees the
// hover before the click (menus/tooltips/web hit-testing).
fn settle() {
    std::thread::sleep(Duration::from_millis(40));
}

// "ctrl+shift+t" → ([Control, Shift], Unicode('t')); "Return" → ([], Return).
pub fn parse_key_combo(combo: &str) -> Result<(Vec<Key>, Key), String> {
    if combo.is_empty() {
        return Err("키가 비어 있습니다".into());
    }
    let parts: Vec<&str> = combo.split('+').map(str::trim).filter(|p| !p.is_empty()).collect();
    if parts.is_empty() {
        // "+" alone means the plus key.
        return Ok((vec![], Key::Unicode('+')));
    }
    let mut mods = Vec::new();
    for m in &parts[..parts.len() - 1] {
        mods.push(match m.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => Key::Control,
            "alt" | "option" | "opt" => Key::Alt,
            "shift" => Key::Shift,
            "cmd" | "command" | "meta" | "super" | "win" => Key::Meta,
            other => return Err(format!("알 수 없는 수식키: {other}")),
        });
    }
    let last = parts[parts.len() - 1];
    Ok((mods, named_key(last)?))
}

fn named_key(name: &str) -> Result<Key, String> {
    let lower = name.to_ascii_lowercase();
    let key = match lower.as_str() {
        "return" | "enter" => Key::Return,
        "tab" => Key::Tab,
        "escape" | "esc" => Key::Escape,
        "backspace" => Key::Backspace,
        "delete" | "del" => Key::Delete,
        "space" => Key::Space,
        "up" | "uparrow" | "arrowup" => Key::UpArrow,
        "down" | "downarrow" | "arrowdown" => Key::DownArrow,
        "left" | "leftarrow" | "arrowleft" => Key::LeftArrow,
        "right" | "rightarrow" | "arrowright" => Key::RightArrow,
        "home" => Key::Home,
        "end" => Key::End,
        "pageup" | "pgup" => Key::PageUp,
        "pagedown" | "pgdn" => Key::PageDown,
        "f1" => Key::F1,
        "f2" => Key::F2,
        "f3" => Key::F3,
        "f4" => Key::F4,
        "f5" => Key::F5,
        "f6" => Key::F6,
        "f7" => Key::F7,
        "f8" => Key::F8,
        "f9" => Key::F9,
        "f10" => Key::F10,
        "f11" => Key::F11,
        "f12" => Key::F12,
        // Modifier pressed alone (e.g. "shift") — click it.
        "ctrl" | "control" => Key::Control,
        "alt" | "option" => Key::Alt,
        "shift" => Key::Shift,
        "cmd" | "command" | "meta" | "super" | "win" => Key::Meta,
        _ => {
            let mut chars = name.chars();
            match (chars.next(), chars.next()) {
                (Some(c), None) => Key::Unicode(c),
                _ => return Err(format!("알 수 없는 키: {name}")),
            }
        }
    };
    Ok(key)
}

// ---- Screenshot -----------------------------------------------------------

fn screenshot(cmd: &ComputerCmd) -> ComputerOutcome {
    let full = match capture_screen() {
        Ok(img) => img,
        Err(e) => return ComputerOutcome::fail(format!("화면 캡처 실패: {e}")),
    };
    let (phys_w, phys_h) = full.dimensions();
    if phys_w == 0 || phys_h == 0 {
        return ComputerOutcome::fail("화면 캡처가 비어 있습니다");
    }
    // Physical px per logical px (Retina 2.0, most X11/Windows 1.0).
    let (logical_w, logical_h) = logical_display_size().unwrap_or((phys_w as i32, phys_h as i32));
    let ppl = if logical_w > 0 { phys_w as f64 / logical_w as f64 } else { 1.0 };
    let _ = logical_h;

    // Region (zoom) is expressed in the previous frame's image space.
    let prev = last_frame();
    let mut img = full;
    let mut origin = (0.0_f64, 0.0_f64); // logical
    let zoom = match (cmd.x, cmd.y, cmd.width, cmd.height) {
        (Some(x), Some(y), Some(w), Some(h)) if w > 0 && h > 0 => Some((x, y, w, h)),
        _ => None,
    };
    if let Some((zx, zy, zw, zh)) = zoom {
        let (lx, ly) = map_to_screen(prev, zx, zy);
        let lw = (zw as f64 * prev.scale).round().max(1.0);
        let lh = (zh as f64 * prev.scale).round().max(1.0);
        // → physical crop, clamped to the capture.
        let px = ((lx as f64) * ppl).round().max(0.0) as u32;
        let py = ((ly as f64) * ppl).round().max(0.0) as u32;
        let pw = ((lw * ppl).round() as u32).clamp(1, phys_w.saturating_sub(px).max(1));
        let ph = ((lh * ppl).round() as u32).clamp(1, phys_h.saturating_sub(py).max(1));
        if px >= phys_w || py >= phys_h {
            return ComputerOutcome::fail("확대 영역이 화면 밖입니다");
        }
        img = image::imageops::crop_imm(&img, px, py, pw, ph).to_image();
        origin = (lx as f64, ly as f64);
    }

    let (cw, ch) = img.dimensions();
    let down = fit_scale(cw, MAX_IMAGE_WIDTH);
    if down < 1.0 {
        let nw = ((cw as f64) * down).round().max(1.0) as u32;
        let nh = ((ch as f64) * down).round().max(1.0) as u32;
        img = image::imageops::resize(&img, nw, nh, FilterType::Triangle);
    }
    let (ow, oh) = img.dimensions();
    // logical px per output image px = (physical per image px) / ppl
    let scale = (1.0 / down) / ppl;
    set_last_frame(Frame { origin_x: origin.0, origin_y: origin.1, scale });

    let mut buf = Cursor::new(Vec::new());
    if let Err(e) = img.write_to(&mut buf, image::ImageFormat::Png) {
        return ComputerOutcome::fail(format!("PNG 인코딩 실패: {e}"));
    }
    ComputerOutcome {
        ok: true,
        image: Some(base64::engine::general_purpose::STANDARD.encode(buf.into_inner())),
        width: Some(ow),
        height: Some(oh),
        ..Default::default()
    }
}

// Logical (point) size of the main display, via enigo — the same space its
// pointer moves use, which is what makes the frame mapping consistent.
fn logical_display_size() -> Option<(i32, i32)> {
    let enigo = Enigo::new(&Settings::default()).ok()?;
    enigo.main_display().ok().filter(|(w, h)| *w > 0 && *h > 0)
}

fn decode_png_file(path: &std::path::Path) -> Result<RgbaImage, String> {
    let bytes = std::fs::read(path).map_err(|e| e.to_string())?;
    let _ = std::fs::remove_file(path);
    image::load_from_memory(&bytes)
        .map(|d| d.to_rgba8())
        .map_err(|e| e.to_string())
}

fn temp_png() -> std::path::PathBuf {
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    std::env::temp_dir().join(format!("andromeda-shot-{nanos}.png"))
}

// Runs a CLI capture tool writing to `path`; Ok(true) if it produced a file.
fn try_cli(program: &str, args: &[&str], path: &std::path::Path) -> bool {
    match std::process::Command::new(program)
        .args(args)
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
    {
        Ok(s) if s.success() => path.exists(),
        _ => false,
    }
}

#[cfg(target_os = "macos")]
fn capture_screen() -> Result<RgbaImage, String> {
    // -x silent, -D 1 main display, -t png. Needs Screen Recording permission
    // (macOS prompts once for Andromeda).
    let path = temp_png();
    let p = path.to_string_lossy().to_string();
    if try_cli("screencapture", &["-x", "-D", "1", "-t", "png", &p], &path) {
        return decode_png_file(&path);
    }
    Err("screencapture 실행 실패 — 시스템 설정 › 개인정보 보호 › 화면 기록에서 Andromeda를 허용했는지 확인".into())
}

#[cfg(target_os = "windows")]
fn capture_screen() -> Result<RgbaImage, String> {
    let path = temp_png();
    let p = path.to_string_lossy().replace('\'', "''");
    let script = format!(
        "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; \
         $b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; \
         $bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height; \
         $g=[System.Drawing.Graphics]::FromImage($bmp); \
         $g.CopyFromScreen($b.Location,[System.Drawing.Point]::Empty,$b.Size); \
         $bmp.Save('{p}',[System.Drawing.Imaging.ImageFormat]::Png)"
    );
    if try_cli("powershell", &["-NoProfile", "-NonInteractive", "-Command", &script], &path) {
        return decode_png_file(&path);
    }
    Err("PowerShell 화면 캡처 실패".into())
}

#[cfg(any(target_os = "linux", target_os = "freebsd"))]
fn capture_screen() -> Result<RgbaImage, String> {
    // X11 first (pure Rust through x11rb, which enigo already links).
    let mut x11_err = String::new();
    if std::env::var_os("DISPLAY").is_some() {
        match capture_x11() {
            Ok(img) => return Ok(img),
            Err(e) => x11_err = e,
        }
    }
    // Wayland / desktop-environment tools, whichever is installed.
    let path = temp_png();
    let p = path.to_string_lossy().to_string();
    let candidates: [(&str, Vec<&str>); 5] = [
        ("grim", vec![&p]),
        ("gnome-screenshot", vec!["-f", &p]),
        ("spectacle", vec!["-b", "-n", "-o", &p]),
        ("import", vec!["-window", "root", &p]),
        ("scrot", vec!["-o", &p]),
    ];
    for (prog, args) in candidates.iter() {
        if try_cli(prog, args, &path) {
            return decode_png_file(&path);
        }
    }
    let mut msg = String::from("사용 가능한 화면 캡처 경로가 없습니다 (X11 GetImage, grim, gnome-screenshot, spectacle, import, scrot 모두 실패)");
    if !x11_err.is_empty() {
        msg.push_str(&format!(" — X11: {x11_err}"));
    }
    Err(msg)
}

#[cfg(any(target_os = "linux", target_os = "freebsd"))]
fn capture_x11() -> Result<RgbaImage, String> {
    use x11rb::connection::Connection;
    use x11rb::protocol::xproto::{ConnectionExt, ImageFormat};
    let (conn, screen_num) = x11rb::connect(None).map_err(|e| e.to_string())?;
    let screen = &conn.setup().roots[screen_num];
    let (w, h) = (screen.width_in_pixels, screen.height_in_pixels);
    let reply = conn
        .get_image(ImageFormat::Z_PIXMAP, screen.root, 0, 0, w, h, !0)
        .map_err(|e| e.to_string())?
        .reply()
        .map_err(|e| e.to_string())?;
    let data = reply.data;
    let (wu, hu) = (w as usize, h as usize);
    if data.len() < wu * hu * 4 {
        return Err(format!("unexpected ZPixmap layout (depth {}, {} bytes)", reply.depth, data.len()));
    }
    let mut img = RgbaImage::new(w as u32, h as u32);
    for (i, px) in img.pixels_mut().enumerate() {
        let o = i * 4;
        // BGRX on little-endian X servers (the overwhelmingly common case).
        px.0 = [data[o + 2], data[o + 1], data[o], 255];
    }
    Ok(img)
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux", target_os = "freebsd")))]
fn capture_screen() -> Result<RgbaImage, String> {
    Err("이 플랫폼에서는 화면 캡처를 지원하지 않습니다".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_mapping_round_trips_with_scale_and_origin() {
        let f = Frame { origin_x: 100.0, origin_y: 50.0, scale: 2.0 };
        assert_eq!(map_to_screen(f, 10, 20), (120, 90));
        assert_eq!(map_to_image(f, 120, 90), (10, 20));
        // Identity frame: image px == screen px.
        assert_eq!(map_to_screen(Frame::default(), 7, 9), (7, 9));
    }

    #[test]
    fn fit_scale_never_upscales_and_caps_width() {
        assert_eq!(fit_scale(800, 1600), 1.0);
        assert_eq!(fit_scale(1600, 1600), 1.0);
        assert!((fit_scale(3200, 1600) - 0.5).abs() < 1e-9);
        assert_eq!(fit_scale(0, 1600), 1.0);
    }

    #[test]
    fn key_combo_parses_modifiers_named_and_unicode_keys() {
        let (mods, key) = parse_key_combo("ctrl+shift+t").unwrap();
        assert_eq!(mods, vec![Key::Control, Key::Shift]);
        assert_eq!(key, Key::Unicode('t'));

        let (mods, key) = parse_key_combo("Return").unwrap();
        assert!(mods.is_empty());
        assert_eq!(key, Key::Return);

        let (mods, key) = parse_key_combo("cmd+Left").unwrap();
        assert_eq!(mods, vec![Key::Meta]);
        assert_eq!(key, Key::LeftArrow);

        let (mods, key) = parse_key_combo("ㄱ").unwrap();
        assert!(mods.is_empty());
        assert_eq!(key, Key::Unicode('ㄱ'));

        assert!(parse_key_combo("").is_err());
        assert!(parse_key_combo("hyper+x").is_err());
        assert!(parse_key_combo("NotAKey").is_err());
    }

    #[test]
    fn run_rejects_unknown_and_reports_missing_coordinates() {
        let out = run(ComputerCmd { action: "format_disk".into(), ..Default::default() });
        assert!(!out.ok);
        assert!(out.error.unwrap().contains("지원하지 않는"));
    }
}
