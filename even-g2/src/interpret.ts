import { REQUEST_TIMEOUT_MS } from "./refresh";
import type { GlanceSettings } from "./settings";

// Live interpretation: glasses microphone → gateway → transcript + translation.
//
// The G2's own Translate is cloud-backed too (Even publishes no engine and no
// latency figure, and reviews name latency as its weak point), so routing to
// Deneb instead is not a detour — and it is the only path that can be given the
// wiki's proper nouns, which is where a business conversation actually breaks.
//
// The device hands the app raw PCM and no transcription, so everything below
// the capture is Deneb's: MOSS for speech, the tiny model role for Korean.

/** What the glasses emit and what the ASR sidecar wants. No resampling anywhere. */
export const SAMPLE_RATE = 16000;

/**
 * How much speech to gather before sending.
 *
 * The floor on end-to-end latency, and the dominant term in it: measured
 * 2026-07-26, transcription of a 3s window takes ~0.3s and translation ~0.5s,
 * so the chunk itself is most of the ~3.8s a wearer waits. Shorter chunks cut
 * the wait but cost accuracy — a sentence split across two windows is
 * transcribed twice, badly, with no context either time.
 */
export const CHUNK_MS = 3_000;

/**
 * RECALL_MS — how much conversation the tap-to-recall buffer keeps.
 *
 * 30s matches what G2 Fact Check retains and is about the span a person can
 * still remember wanting to capture. At 16 kHz mono 16-bit that is ~0.96 MB,
 * comfortably inside the gateway's 4 MiB clip limit.
 */
export const RECALL_MS = 30_000;

/** RECALL_BYTES — the recall buffer's retention in bytes. */
export const RECALL_BYTES = (SAMPLE_RATE * 2 * RECALL_MS) / 1000;

/** Refuse to buffer past this, whatever the host does with the mic. */
const MAX_CHUNK_BYTES = (SAMPLE_RATE * 2 * CHUNK_MS * 3) / 1000;

export type InterpretResult = {
  text: string;
  translation?: string;
  /** Deneb's answer, when the request carried `ask`. */
  reply?: string;
  askError?: string;
  /** Which model answered and how long the turn took — the HUD's debug panel. */
  model?: string;
  ms?: number;
};

/**
 * PcmBuffer accumulates audio events into send-sized windows.
 *
 * Pure, so the part that can be tested without a microphone is tested: the
 * device delivering PCM at all is only observable on real glasses.
 */
export class PcmBuffer {
  private parts: Uint8Array[] = [];
  private bytes = 0;

  /**
   * @param cap bytes to retain. The default is one interpretation window's
   * worth; a recall buffer passes a much larger value because it must hold the
   * last half-minute of a conversation, not the last sentence.
   */
  constructor(private readonly cap: number = MAX_CHUNK_BYTES) {}

  /** Bytes that make up one window at the configured chunk length. */
  static readonly windowBytes = (SAMPLE_RATE * 2 * CHUNK_MS) / 1000;

  push(sample: Uint8Array): void {
    if (!sample?.length) return;
    this.parts.push(sample);
    this.bytes += sample.length;
    // A host that never stops sending must not grow this without bound.
    while (this.bytes > this.cap && this.parts.length > 1) {
      this.bytes -= this.parts.shift()!.length;
    }
  }

  get length(): number {
    return this.bytes;
  }

  ready(): boolean {
    return this.bytes >= PcmBuffer.windowBytes;
  }

  /**
   * snapshot copies without draining.
   *
   * Recall taps must NOT consume the buffer: the wearer taps because something
   * was just said, and a second thought a few seconds later has to still find
   * that audio there. `take` is for the interpretation loop, which wants each
   * window exactly once.
   */
  snapshot(): Uint8Array | null {
    if (this.bytes === 0) return null;
    const out = new Uint8Array(this.bytes);
    let at = 0;
    for (const p of this.parts) {
      out.set(p, at);
      at += p.length;
    }
    return out;
  }

  /** take drains everything buffered so far, or null when there is nothing. */
  take(): Uint8Array | null {
    if (this.bytes === 0) return null;
    const out = new Uint8Array(this.bytes);
    let at = 0;
    for (const p of this.parts) {
      out.set(p, at);
      at += p.length;
    }
    this.parts = [];
    this.bytes = 0;
    return out;
  }
}

/** base64 without a Buffer polyfill — the WebView has btoa and not much else. */
export function toBase64(bytes: Uint8Array): string {
  let s = "";
  // Chunked: btoa on a spread of a large array overflows the argument limit.
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}

/** postAudio ships one window and returns what came back. */
export async function postAudio(
  settings: GlanceSettings,
  pcm: Uint8Array,
  opts?: { translateTo?: string; hotwords?: string; ask?: string },
): Promise<InterpretResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const res = await fetch(`${settings.baseUrl}/api/even/audio`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${settings.token}`,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({
        pcm: toBase64(pcm),
        sampleRate: SAMPLE_RATE,
        translateTo: opts?.translateTo ?? "",
        hotwords: opts?.hotwords ?? "",
        ask: opts?.ask ?? "",
      }),
      signal: controller.signal,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = (await res.json()) as { text?: string; translation?: string };
    return {
      text: String(data.text ?? "").trim(),
      translation: data.translation?.trim() || undefined,
    };
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError")
      throw new Error("시간 초과");
    throw err;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * postTextAsk hands words Deneb already has (an interpretation session's
 * accumulated subtitles) to a chat turn, without re-sending audio to get the
 * same words back.
 */
export async function postTextAsk(
  settings: GlanceSettings,
  text: string,
  ask: string,
): Promise<InterpretResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const res = await fetch(`${settings.baseUrl}/api/even/audio`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${settings.token}`,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({ text, ask }),
      signal: controller.signal,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = (await res.json()) as {
      reply?: string;
      askError?: string;
      model?: string;
      ms?: number;
    };
    return {
      text,
      reply: data.reply?.trim() || undefined,
      askError: data.askError?.trim() || undefined,
      model: data.model?.trim() || undefined,
      ms: typeof data.ms === "number" ? data.ms : undefined,
    };
  } finally {
    clearTimeout(timer);
  }
}

/**
 * subtitleLines keeps the last few renderings on screen.
 *
 * A HUD subtitle that replaces itself every window is unreadable — the wearer
 * looks up mid-sentence and the sentence is gone. Keeping a short tail means a
 * glance still catches what was just said.
 */
export function subtitleLines(
  history: string[],
  incoming: string,
  keep = 3,
): string[] {
  const t = incoming.trim();
  if (!t) return history;
  return [...history, t].slice(-keep);
}
