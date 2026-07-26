import { describe, expect, it } from "vitest";
import {
  CHUNK_MS,
  PcmBuffer,
  SAMPLE_RATE,
  subtitleLines,
  toBase64,
} from "./interpret";

const bytes = (n: number, fill = 1) => new Uint8Array(n).fill(fill);

describe("PcmBuffer", () => {
  it("waits for a full window before saying it is ready", () => {
    const b = new PcmBuffer();
    expect(PcmBuffer.windowBytes).toBe((SAMPLE_RATE * 2 * CHUNK_MS) / 1000);
    b.push(bytes(PcmBuffer.windowBytes - 1));
    expect(b.ready()).toBe(false);
    b.push(bytes(1));
    expect(b.ready()).toBe(true);
  });

  it("reassembles the events in order", () => {
    const b = new PcmBuffer();
    b.push(Uint8Array.from([1, 2]));
    b.push(Uint8Array.from([3]));
    b.push(Uint8Array.from([4, 5]));
    expect(Array.from(b.take()!)).toEqual([1, 2, 3, 4, 5]);
  });

  it("empties on take", () => {
    const b = new PcmBuffer();
    b.push(bytes(10));
    b.take();
    expect(b.length).toBe(0);
    expect(b.take()).toBeNull();
  });

  it("ignores empty events instead of counting them", () => {
    const b = new PcmBuffer();
    b.push(new Uint8Array(0));
    expect(b.length).toBe(0);
    expect(b.take()).toBeNull();
  });

  it("drops the oldest audio rather than growing without bound", () => {
    // A host that opens the mic and never stops must not turn the WebView into
    // a memory leak; the wearer wants what was just said, not what was said
    // three minutes ago.
    const b = new PcmBuffer();
    for (let i = 0; i < 20; i++) b.push(bytes(PcmBuffer.windowBytes));
    expect(b.length).toBeLessThanOrEqual(PcmBuffer.windowBytes * 3);
  });
});

describe("toBase64", () => {
  it("round-trips", () => {
    const src = Uint8Array.from([0, 1, 2, 253, 254, 255]);
    const back = Uint8Array.from(atob(toBase64(src)), (c) => c.charCodeAt(0));
    expect(Array.from(back)).toEqual(Array.from(src));
  });

  it("survives a full window without blowing the argument limit", () => {
    // String.fromCharCode(...bytes) on a 96 KB window throws; the encoder has
    // to chunk, and a 3-second window is exactly the size that would break it.
    const big = bytes(PcmBuffer.windowBytes, 7);
    expect(() => toBase64(big)).not.toThrow();
    expect(atob(toBase64(big)).length).toBe(big.length);
  });
});

describe("subtitleLines", () => {
  it("keeps a short tail so a glance catches the last sentence", () => {
    let h: string[] = [];
    for (const s of ["하나", "둘", "셋", "넷"]) h = subtitleLines(h, s);
    expect(h).toEqual(["둘", "셋", "넷"]);
  });

  it("ignores blank renderings", () => {
    expect(subtitleLines(["하나"], "   ")).toEqual(["하나"]);
  });
});

describe("PcmBuffer recall window", () => {
  it("retains up to the capacity and drops the oldest beyond it", () => {
    const b = new PcmBuffer(1000);
    for (let i = 0; i < 5; i += 1) b.push(new Uint8Array(400));
    // 2000 bytes pushed, capacity 1000 — the oldest chunks are gone.
    expect(b.length).toBeLessThanOrEqual(1000);
    expect(b.length).toBeGreaterThan(0);
  });

  it("snapshot does not drain, take does", () => {
    // The recall tap must be repeatable: a wearer who taps, reads, and thinks of
    // a follow-up seconds later has to still find the audio there.
    const b = new PcmBuffer(1000);
    b.push(new Uint8Array(100));
    const first = b.snapshot();
    expect(first?.length).toBe(100);
    expect(b.length).toBe(100);
    const second = b.snapshot();
    expect(second?.length).toBe(100);
    b.take();
    expect(b.length).toBe(0);
    expect(b.snapshot()).toBeNull();
  });

  it("defaults to the interpretation window when no capacity is given", () => {
    const b = new PcmBuffer();
    b.push(new Uint8Array(PcmBuffer.windowBytes));
    expect(b.ready()).toBe(true);
  });
});
