mod ws_client;

use propus_common::{ClientMessage, ServerMessage};
use slint::{Model, ModelRc, SharedString, VecModel};
use std::io::Write;
use std::sync::Arc;
use tokio::sync::{Mutex, mpsc};
use tokio::time::Duration;

slint::include_modules!();

#[tokio::main]
async fn main() {
    let app = App::new().unwrap();

    // Shared sender to server (set after connection)
    let ws_tx: Arc<Mutex<Option<mpsc::UnboundedSender<ClientMessage>>>> =
        Arc::new(Mutex::new(None));

    // Saved server URL for reconnection
    let server_url: Arc<Mutex<Option<String>>> = Arc::new(Mutex::new(None));

    // Load saved server URL
    let saved_url = load_client_config();
    if let Some(ref url) = saved_url {
        *server_url.blocking_lock() = Some(url.clone());
        app.set_status_text(SharedString::from(&format!("저장된 서버: {url}")));
    }

    // Restore last session's history.
    let history = load_history(100);
    if !history.is_empty() {
        let vec_model = std::rc::Rc::new(VecModel::<ChatMessage>::default());
        for (role, content) in &history {
            vec_model.push(ChatMessage {
                role: SharedString::from(role.as_str()),
                content: SharedString::from(content.as_str()),
                is_tool: false,
                tool_name: SharedString::default(),
                is_expanded: false,
            });
        }
        app.set_msg_count(vec_model.row_count() as i32);
        app.set_messages(ModelRc::from(vec_model));
    }

    // ─── Connect to server ───
    let app_weak = app.as_weak();
    let ws_tx_clone = ws_tx.clone();
    let server_url_clone = server_url.clone();
    app.on_connect_server(move |url| {
        let url = url.to_string().trim().to_string();
        if url.is_empty() {
            return;
        }

        let app_weak = app_weak.clone();
        let ws_tx = ws_tx_clone.clone();
        let server_url = server_url_clone.clone();

        // Save URL for next time
        let _ = save_client_config(&url);

        if let Some(app) = app_weak.upgrade() {
            app.set_status_text(SharedString::from("연결 중..."));
        }

        tokio::spawn(async move {
            *server_url.lock().await = Some(url.clone());
            match ws_client::connect(&url).await {
                Ok(handle) => {
                    let tx = handle.tx.clone();
                    *ws_tx.lock().await = Some(handle.tx);

                    let aw = app_weak.clone();
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = aw.upgrade() {
                            app.set_needs_server_url(false);
                            app.set_connection_status(SharedString::from("connected"));
                            app.set_status_text(SharedString::from("서버 연결됨, 설정 확인 중..."));
                        }
                    });

                    // Start heartbeat ping loop.
                    spawn_heartbeat(tx);
                    // Start receiving messages
                    spawn_receiver(app_weak, handle.rx);
                }
                Err(e) => {
                    let aw = app_weak.clone();
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = aw.upgrade() {
                            app.set_status_text(SharedString::from(&format!("연결 실패: {e}")));
                        }
                    });
                }
            }
        });
    });

    // ─── Send message ───
    let app_weak = app.as_weak();
    let ws_tx_clone = ws_tx.clone();
    app.on_send_message(move |text| {
        let text = text.to_string().trim().to_string();
        if text.is_empty() {
            return;
        }

        if let Some(app) = app_weak.upgrade() {
            push_message(&app, "user", &text, false, "");
            app.set_is_streaming(true);
            app.set_status_text(SharedString::from("생성 중..."));
            app.set_streaming_text(SharedString::default());
            app.set_current_segments(ModelRc::new(VecModel::from(Vec::<MessageSegment>::new())));
        }

        let ws_tx = ws_tx_clone.clone();
        tokio::spawn(async move {
            if let Some(tx) = ws_tx.lock().await.as_ref() {
                let _ = tx.send(ClientMessage::SendMessage { text });
            }
        });
    });

    // ─── Stop generation ───
    let ws_tx_clone = ws_tx.clone();
    let app_weak = app.as_weak();
    app.on_stop_generation(move || {
        let ws_tx = ws_tx_clone.clone();
        let app_weak = app_weak.clone();
        tokio::spawn(async move {
            if let Some(tx) = ws_tx.lock().await.as_ref() {
                let _ = tx.send(ClientMessage::StopGeneration);
            }
            let _ = slint::invoke_from_event_loop(move || {
                if let Some(app) = app_weak.upgrade() {
                    app.set_is_streaming(false);
                    app.set_status_text(SharedString::from("중지됨"));
                }
            });
        });
    });

    // ─── Clear chat ───
    let app_weak = app.as_weak();
    let ws_tx_clone = ws_tx.clone();
    app.on_clear_chat(move || {
        let app_weak = app_weak.clone();
        let ws_tx = ws_tx_clone.clone();
        tokio::spawn(async move {
            if let Some(tx) = ws_tx.lock().await.as_ref() {
                let _ = tx.send(ClientMessage::ClearChat);
            }
            let _ = slint::invoke_from_event_loop(move || {
                if let Some(app) = app_weak.upgrade() {
                    app.set_messages(ModelRc::new(VecModel::from(Vec::<ChatMessage>::new())));
                    app.set_streaming_text(SharedString::default());
                    app.set_current_segments(ModelRc::new(VecModel::from(
                        Vec::<MessageSegment>::new(),
                    )));
                    app.set_usage_text(SharedString::default());
                    app.set_msg_count(0);
                    app.set_status_text(SharedString::from("대화 초기화됨"));
                    clear_history();
                }
            });
        });
    });

    // ─── Save session ───
    let ws_tx_clone = ws_tx.clone();
    app.on_save_session(move || {
        let ws_tx = ws_tx_clone.clone();
        tokio::spawn(async move {
            if let Some(tx) = ws_tx.lock().await.as_ref() {
                let _ = tx.send(ClientMessage::SaveSession);
            }
        });
    });

    // ─── Submit API key ───
    let ws_tx_clone = ws_tx.clone();
    app.on_submit_api_key(move |key| {
        let key = key.to_string().trim().to_string();
        if key.is_empty() {
            return;
        }
        let ws_tx = ws_tx_clone.clone();
        tokio::spawn(async move {
            if let Some(tx) = ws_tx.lock().await.as_ref() {
                let _ = tx.send(ClientMessage::SetApiKey { key });
            }
        });
    });

    // ─── Reconnect ───
    let app_weak = app.as_weak();
    let ws_tx_clone = ws_tx.clone();
    let server_url_clone = server_url.clone();
    app.on_reconnect(move || {
        let app_weak = app_weak.clone();
        let ws_tx = ws_tx_clone.clone();
        let server_url = server_url_clone.clone();

        tokio::spawn(async move {
            let url = {
                let guard = server_url.lock().await;
                guard.clone()
            };

            let Some(url) = url else {
                let aw = app_weak.clone();
                let _ = slint::invoke_from_event_loop(move || {
                    if let Some(app) = aw.upgrade() {
                        app.set_needs_server_url(true);
                    }
                });
                return;
            };

            // Try reconnection with exponential backoff (3 attempts)
            for attempt in 0..3 {
                if attempt > 0 {
                    tokio::time::sleep(Duration::from_millis(1000 * 2u64.pow(attempt))).await;
                }

                let aw = app_weak.clone();
                let attempt_num = attempt + 1;
                let _ = slint::invoke_from_event_loop(move || {
                    if let Some(app) = aw.upgrade() {
                        app.set_status_text(SharedString::from(&format!(
                            "재연결 시도 중... ({attempt_num}/3)"
                        )));
                    }
                });

                match ws_client::connect(&url).await {
                    Ok(handle) => {
                        let tx = handle.tx.clone();
                        *ws_tx.lock().await = Some(handle.tx);
                        let aw = app_weak.clone();
                        let _ = slint::invoke_from_event_loop(move || {
                            if let Some(app) = aw.upgrade() {
                                app.set_connection_status(SharedString::from("connected"));
                                app.set_status_text(SharedString::from("서버 재연결됨"));
                            }
                        });
                        spawn_heartbeat(tx);
                        spawn_receiver(app_weak, handle.rx);
                        return;
                    }
                    Err(_) => continue,
                }
            }

            let aw = app_weak.clone();
            let _ = slint::invoke_from_event_loop(move || {
                if let Some(app) = aw.upgrade() {
                    app.set_status_text(SharedString::from("재연결 실패"));
                }
            });
        });
    });

    // ─── Scroll to bottom (no-op placeholder, ScrollView auto-manages) ───
    app.on_request_scroll_to_bottom(|| {
        // Slint ScrollView manages viewport; this callback exists for future use
    });

    // If we have a saved URL, auto-connect
    if let Some(url) = saved_url {
        let app_weak = app.as_weak();
        let ws_tx_clone = ws_tx.clone();
        tokio::spawn(async move {
            match ws_client::connect(&url).await {
                Ok(handle) => {
                    let tx = handle.tx.clone();
                    *ws_tx_clone.lock().await = Some(handle.tx);
                    let aw = app_weak.clone();
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = aw.upgrade() {
                            app.set_needs_server_url(false);
                            app.set_connection_status(SharedString::from("connected"));
                            app.set_status_text(SharedString::from("서버 연결됨"));
                        }
                    });
                    spawn_heartbeat(tx);
                    spawn_receiver(app_weak, handle.rx);
                }
                Err(_) => {
                    let aw = app_weak.clone();
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = aw.upgrade() {
                            app.set_status_text(SharedString::from(
                                &format!("자동 연결 실패 — 서버 주소를 확인하세요 ({url})"),
                            ));
                        }
                    });
                }
            }
        });
    }

    app.run().unwrap();
}

/// Spawn a task that receives ServerMessages and updates the Slint UI.
fn spawn_receiver(
    app_weak: slint::Weak<App>,
    mut rx: mpsc::UnboundedReceiver<ServerMessage>,
) {
    let streaming_buf = Arc::new(Mutex::new(String::new()));

    tokio::spawn(async move {
        while let Some(msg) = rx.recv().await {
            let app_weak = app_weak.clone();
            let buf = streaming_buf.clone();

            match msg {
                ServerMessage::Text { content } => {
                    buf.lock().await.push_str(&content);
                    let text = buf.lock().await.clone();
                    let segments = parse_message_segments(&text);
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            app.set_streaming_text(SharedString::from(&text));
                            app.set_current_segments(ModelRc::new(VecModel::from(segments)));
                        }
                    });
                }
                ServerMessage::ToolStart { name, args } => {
                    // Finalize streaming text before tool block
                    let pending = {
                        let mut b = buf.lock().await;
                        let t = b.clone();
                        b.clear();
                        t
                    };
                    let summary = safe_truncate(&args, 120);
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            if !pending.is_empty() {
                                push_message(&app, "assistant", &pending, false, "");
                                app.set_streaming_text(SharedString::default());
                                app.set_current_segments(ModelRc::new(VecModel::from(
                                    Vec::<MessageSegment>::new(),
                                )));
                            }
                            push_message(
                                &app,
                                "tool",
                                &format!("{summary}"),
                                true,
                                &format!("{name}"),
                            );
                        }
                    });
                }
                ServerMessage::ToolResult { name, result } => {
                    let short = safe_truncate(&result, 800);
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            push_message(&app, "tool", &short, true, &format!("{name} ✓"));
                        }
                    });
                }
                ServerMessage::Usage {
                    prompt,
                    completion,
                    total,
                } => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            app.set_usage_text(SharedString::from(&format!(
                                "입력 {prompt}  출력 {completion}  합계 {total}"
                            )));
                        }
                    });
                }
                ServerMessage::Done => {
                    let final_text = {
                        let mut b = buf.lock().await;
                        let t = b.clone();
                        b.clear();
                        t
                    };
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            if !final_text.is_empty() {
                                push_message(&app, "assistant", &final_text, false, "");
                            }
                            app.set_streaming_text(SharedString::default());
                            app.set_current_segments(ModelRc::new(VecModel::from(
                                Vec::<MessageSegment>::new(),
                            )));
                            app.set_is_streaming(false);
                            app.set_status_text(SharedString::from("준비됨"));
                        }
                    });
                }
                ServerMessage::Error { message } => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            push_message(
                                &app,
                                "assistant",
                                &format!("오류: {message}"),
                                false,
                                "",
                            );
                            app.set_streaming_text(SharedString::default());
                            app.set_current_segments(ModelRc::new(VecModel::from(
                                Vec::<MessageSegment>::new(),
                            )));
                            app.set_is_streaming(false);
                            app.set_status_text(SharedString::from("오류 발생"));
                        }
                    });
                }
                ServerMessage::SessionSaved { path } => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            app.set_status_text(SharedString::from(&format!(
                                "세션 저장됨: {path}"
                            )));
                        }
                    });
                }
                ServerMessage::ChatCleared => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            app.set_messages(ModelRc::new(VecModel::from(
                                Vec::<ChatMessage>::new(),
                            )));
                            app.set_streaming_text(SharedString::default());
                            app.set_current_segments(ModelRc::new(VecModel::from(
                                Vec::<MessageSegment>::new(),
                            )));
                            app.set_usage_text(SharedString::default());
                            app.set_msg_count(0);
                            app.set_status_text(SharedString::from("대화 초기화됨"));
                            clear_history();
                        }
                    });
                }
                ServerMessage::ConfigStatus {
                    needs_api_key,
                    model,
                    service,
                    deneb_status,
                } => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            app.set_needs_api_key(needs_api_key);
                            if !needs_api_key {
                                app.set_model_name(SharedString::from(&model));
                                app.set_service_name(SharedString::from(&service));
                                let status = if deneb_status.is_empty() {
                                    "준비됨".to_string()
                                } else {
                                    format!("준비됨 (Deneb {deneb_status})")
                                };
                                app.set_status_text(SharedString::from(&status));
                            }
                        }
                    });
                }
                ServerMessage::Pong => {}
                ServerMessage::File {
                    name,
                    media_type,
                    size,
                    url,
                } => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            let size_str = format_file_size(size);
                            let msg = format!("📎 {name} ({media_type}, {size_str})\n{url}");
                            push_message(&app, "assistant", &msg, false, "");
                        }
                    });
                }
                ServerMessage::Typing => {
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(app) = app_weak.upgrade() {
                            if !app.get_is_streaming() {
                                app.set_is_streaming(true);
                                app.set_status_text(SharedString::from("생성 중..."));
                            }
                        }
                    });
                }
            }
        }

        // WebSocket closed — update UI
        let aw = app_weak.clone();
        let _ = slint::invoke_from_event_loop(move || {
            if let Some(app) = aw.upgrade() {
                app.set_connection_status(SharedString::from("disconnected"));
                app.set_status_text(SharedString::from("서버 연결 끊김"));
                app.set_is_streaming(false);
            }
        });
    });
}

// ─── Message Segment Parsing ───

/// Parse message content into text and code segments.
/// Splits on ``` boundaries for distinct rendering.
fn parse_message_segments(content: &str) -> Vec<MessageSegment> {
    let mut segments = Vec::new();
    let mut remaining = content;

    while let Some(start) = remaining.find("```") {
        // Text before code block
        let before = &remaining[..start];
        if !before.is_empty() {
            segments.push(MessageSegment {
                text: SharedString::from(before.trim_end()),
                is_code: false,
                language: SharedString::default(),
            });
        }

        let after_fence = &remaining[start + 3..];

        // Extract language from opening fence
        let (language, code_start) = if let Some(nl) = after_fence.find('\n') {
            let lang = after_fence[..nl].trim().to_string();
            (lang, nl + 1)
        } else {
            // Incomplete fence — just show as text
            segments.push(MessageSegment {
                text: SharedString::from(remaining),
                is_code: false,
                language: SharedString::default(),
            });
            return segments;
        };

        let code_content = &after_fence[code_start..];

        // Find closing fence
        if let Some(end) = code_content.find("```") {
            let code = &code_content[..end];
            segments.push(MessageSegment {
                text: SharedString::from(code.trim_end()),
                is_code: true,
                language: SharedString::from(&language),
            });
            remaining = &code_content[end + 3..];
        } else {
            // Unclosed code block (still streaming) — show as code
            segments.push(MessageSegment {
                text: SharedString::from(code_content.trim_end()),
                is_code: true,
                language: SharedString::from(&language),
            });
            return segments;
        }
    }

    // Remaining text after last code block
    if !remaining.is_empty() {
        segments.push(MessageSegment {
            text: SharedString::from(remaining.trim_start_matches('\n')),
            is_code: false,
            language: SharedString::default(),
        });
    }

    segments
}

// ─── Helpers ───

fn push_message(app: &App, role: &str, content: &str, is_tool: bool, tool_name: &str) {
    let messages = app.get_messages();
    let vec_model = clone_model(&messages);
    vec_model.push(ChatMessage {
        role: SharedString::from(role),
        content: SharedString::from(content),
        is_tool,
        tool_name: SharedString::from(tool_name),
        is_expanded: false,
    });
    let count = vec_model.row_count() as i32;
    app.set_messages(ModelRc::from(vec_model));
    app.set_msg_count(count);

    // Persist to local history (skip tool messages to keep history concise).
    if !is_tool {
        append_history(role, content);
    }
}

fn clone_model(model: &ModelRc<ChatMessage>) -> std::rc::Rc<VecModel<ChatMessage>> {
    let items: Vec<ChatMessage> = (0..model.row_count())
        .filter_map(|i| model.row_data(i))
        .collect();
    std::rc::Rc::new(VecModel::from(items))
}

fn safe_truncate(s: &str, max_chars: usize) -> String {
    if s.chars().count() <= max_chars {
        return s.to_string();
    }
    let lines: Vec<&str> = s.lines().collect();
    if lines.len() > 8 {
        let preview: String = lines[..6].join("\n");
        return format!("{preview}\n... ({} more lines)", lines.len() - 6);
    }
    let truncated: String = s.chars().take(max_chars).collect();
    format!("{truncated}...")
}

// ─── Heartbeat ───

/// Spawn a background task that sends Ping every 25 seconds to keep the connection alive.
fn spawn_heartbeat(tx: mpsc::UnboundedSender<ClientMessage>) {
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(Duration::from_secs(25));
        loop {
            interval.tick().await;
            if tx.send(ClientMessage::Ping).is_err() {
                break; // Channel closed, connection dropped
            }
        }
    });
}

// ─── File Size Formatting ───

fn format_file_size(bytes: i64) -> String {
    if bytes < 1024 {
        format!("{bytes} B")
    } else if bytes < 1024 * 1024 {
        format!("{:.1} KB", bytes as f64 / 1024.0)
    } else {
        format!("{:.1} MB", bytes as f64 / (1024.0 * 1024.0))
    }
}

// ─── Local History ───

fn history_dir() -> std::path::PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| std::path::PathBuf::from("."))
        .join(".propus")
        .join("history")
}

fn current_history_path() -> std::path::PathBuf {
    history_dir().join("current.jsonl")
}

/// Append a message to the local history file.
fn append_history(role: &str, content: &str) {
    let path = current_history_path();
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let entry = serde_json::json!({
        "role": role,
        "content": content,
        "ts": chrono::Utc::now().to_rfc3339(),
    });
    if let Ok(mut f) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
    {
        let _ = writeln!(f, "{}", entry);
    }
}

/// Load the last N messages from local history.
fn load_history(limit: usize) -> Vec<(String, String)> {
    let path = current_history_path();
    let data = match std::fs::read_to_string(&path) {
        Ok(d) => d,
        Err(_) => return Vec::new(),
    };
    let mut messages = Vec::new();
    for line in data.lines() {
        if line.is_empty() {
            continue;
        }
        if let Ok(entry) = serde_json::from_str::<serde_json::Value>(line) {
            let role = entry
                .get("role")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .to_string();
            let content = entry
                .get("content")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .to_string();
            if !role.is_empty() && !content.is_empty() {
                messages.push((role, content));
            }
        }
    }
    // Return only the last `limit` messages.
    if messages.len() > limit {
        messages.split_off(messages.len() - limit)
    } else {
        messages
    }
}

/// Clear local history file.
fn clear_history() {
    let path = current_history_path();
    let _ = std::fs::remove_file(path);
}

// ─── Client Config ───

fn client_config_path() -> std::path::PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| std::path::PathBuf::from("."))
        .join(".propus")
        .join("client.json")
}

fn load_client_config() -> Option<String> {
    let path = client_config_path();
    let data = std::fs::read_to_string(&path).ok()?;
    let config: serde_json::Value = serde_json::from_str(&data).ok()?;
    config
        .get("server_url")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string())
}

fn save_client_config(server_url: &str) -> Result<(), String> {
    let path = client_config_path();
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir).map_err(|e| format!("{e}"))?;
    }
    let json = serde_json::json!({ "server_url": server_url });
    let data = serde_json::to_string_pretty(&json).map_err(|e| format!("{e}"))?;
    std::fs::write(&path, data).map_err(|e| format!("{e}"))?;
    Ok(())
}
