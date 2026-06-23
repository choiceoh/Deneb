// Shared Server-Sent Events reader. Both chat/stream and the proactive events
// channel use the same `event:` / `data:` line framing, so this is the single
// parser they share — feed it a response body, get one callback per frame.
//
// (We read SSE off fetch() rather than EventSource because EventSource can't set
// the X-Deneb-Client-Token header. Aborting the fetch ends the read loop.)
export interface SSEFrame {
  event: string; // the `event:` name, or "" if the frame had none
  data: string; // the raw `data:` payload (usually JSON — caller parses)
}

export async function readSSE(body: ReadableStream<Uint8Array>, onFrame: (frame: SSEFrame) => void): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // keep the trailing partial line for the next chunk
    for (const line of lines) {
      if (line.startsWith("event:")) {
        event = line.slice(6).trim();
        continue;
      }
      if (!line.startsWith("data:")) continue;
      const data = line.slice(5).trim();
      if (!data) continue;
      onFrame({ event, data });
      event = ""; // event name applies to a single frame
    }
  }
}

// readSSE + per-frame JSON.parse, with malformed frames silently skipped. Both
// consumers (chat/stream and the events channel) want the parsed object; only the
// per-event handling differs, so they share this and just switch on `event`.
export async function readJsonSSE(
  body: ReadableStream<Uint8Array>,
  onObj: (event: string, obj: Record<string, unknown>) => void,
): Promise<void> {
  await readSSE(body, ({ event, data }) => {
    let obj: Record<string, unknown>;
    try {
      obj = JSON.parse(data) as Record<string, unknown>;
    } catch {
      return; // ignore malformed frame
    }
    onObj(event, obj);
  });
}
