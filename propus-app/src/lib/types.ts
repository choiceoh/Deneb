// ═══ WebSocket protocol types — must match gateway-go/internal/propus/types.go ═══

// --- Client → Server ---

export type ClientMessage =
  | { type: "SendMessage"; data: { text: string } }
  | { type: "ClearChat" }
  | { type: "SaveSession" }
  | { type: "SetApiKey"; data: { key: string } }
  | { type: "StopGeneration" }
  | { type: "Ping" };

// --- Server → Client ---

export type ServerMessage =
  | { type: "Text"; data: { content: string } }
  | { type: "ToolStart"; data: { name: string; args: string } }
  | { type: "ToolResult"; data: { name: string; result: string } }
  | { type: "Usage"; data: { prompt: number; completion: number; total: number } }
  | { type: "Done" }
  | { type: "Error"; data: { message: string } }
  | { type: "SessionSaved"; data: { path: string } }
  | { type: "ChatCleared" }
  | {
      type: "ConfigStatus";
      data: {
        needs_api_key: boolean;
        model: string;
        service: string;
        deneb_status: string;
      };
    }
  | {
      type: "File";
      data: { name: string; media_type: string; size: number; url: string };
    }
  | { type: "Typing" }
  | { type: "Pong" };

// --- App-level types ---

export interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "tool" | "file";
  content: string;
  /** Parsed content segments for rendering (only for assistant messages). */
  segments: ContentSegment[];
  toolName?: string;
  toolResult?: string;
  expanded?: boolean;
  /** File message fields. */
  fileName?: string;
  fileUrl?: string;
  fileSize?: number;
  fileMediaType?: string;
}

export interface ContentSegment {
  type: "text" | "code";
  content: string;
  language?: string;
}

// Parse raw text into segments (splitting on ``` fences).
export function parseSegments(text: string): ContentSegment[] {
  const segments: ContentSegment[] = [];
  let remaining = text;

  while (true) {
    const fenceStart = remaining.indexOf("```");
    if (fenceStart === -1) break;

    // Text before the fence
    const before = remaining.slice(0, fenceStart);
    if (before.trim()) {
      segments.push({ type: "text", content: before.trimEnd() });
    }

    const afterFence = remaining.slice(fenceStart + 3);
    const newline = afterFence.indexOf("\n");
    if (newline === -1) {
      // Incomplete fence (still streaming) — treat rest as text
      segments.push({ type: "text", content: remaining });
      return segments;
    }

    const language = afterFence.slice(0, newline).trim();
    const codeContent = afterFence.slice(newline + 1);
    const fenceEnd = codeContent.indexOf("```");

    if (fenceEnd === -1) {
      // Unclosed code block (still streaming)
      segments.push({
        type: "code",
        content: codeContent.trimEnd(),
        language: language || undefined,
      });
      return segments;
    }

    segments.push({
      type: "code",
      content: codeContent.slice(0, fenceEnd).trimEnd(),
      language: language || undefined,
    });
    remaining = codeContent.slice(fenceEnd + 3);
  }

  // Remaining text
  if (remaining.trim()) {
    segments.push({
      type: "text",
      content: remaining.replace(/^\n/, ""),
    });
  }

  return segments;
}
