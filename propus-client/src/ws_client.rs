use propus_common::{ClientMessage, ServerMessage};
use futures_util::{SinkExt, StreamExt};
use tokio::sync::mpsc;
use tokio_tungstenite::{connect_async, tungstenite::Message};

pub struct WsHandle {
    pub tx: mpsc::UnboundedSender<ClientMessage>,
    pub rx: mpsc::UnboundedReceiver<ServerMessage>,
}

/// Connect to the Propus server WebSocket.
/// Returns a handle with send/receive channels.
pub async fn connect(url: &str) -> Result<WsHandle, String> {
    let (ws_stream, _) = connect_async(url)
        .await
        .map_err(|e| format!("WebSocket 연결 실패: {e}"))?;

    let (mut ws_tx, mut ws_rx) = ws_stream.split();

    // Client → Server channel
    let (client_tx, mut client_rx) = mpsc::unbounded_channel::<ClientMessage>();
    // Server → Client channel
    let (server_tx, server_rx) = mpsc::unbounded_channel::<ServerMessage>();

    // Writer: send ClientMessage to WebSocket
    tokio::spawn(async move {
        while let Some(msg) = client_rx.recv().await {
            let json = match serde_json::to_string(&msg) {
                Ok(j) => j,
                Err(_) => continue,
            };
            if ws_tx.send(Message::Text(json.into())).await.is_err() {
                break;
            }
        }
    });

    // Reader: receive ServerMessage from WebSocket
    tokio::spawn(async move {
        while let Some(Ok(msg)) = ws_rx.next().await {
            match msg {
                Message::Text(text) => {
                    if let Ok(server_msg) = serde_json::from_str::<ServerMessage>(&text)
                        && server_tx.send(server_msg).is_err() {
                            break;
                    }
                }
                Message::Close(_) => break,
                _ => {}
            }
        }
    });

    Ok(WsHandle {
        tx: client_tx,
        rx: server_rx,
    })
}
