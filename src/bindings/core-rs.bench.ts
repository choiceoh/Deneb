import crypto from "node:crypto";
import { bench, describe } from "vitest";
import { loadCoreRs } from "./core-rs.js";

const mod = loadCoreRs();

describe("core-rs native vs JS performance", () => {
  // --- validate_frame ---
  const validReqJson =
    '{"type":"req","id":"abc-123","method":"chat.send","params":{"text":"hello world"}}';

  bench("validateFrame (native)", () => {
    mod.validateFrame(validReqJson);
  });

  // --- constant_time_eq ---
  const secretA = Buffer.from("a]3kF!9x@Lm#pQ7z&wR2$vY5^tN8*hG");
  const secretB = Buffer.from("a]3kF!9x@Lm#pQ7z&wR2$vY5^tN8*hG");

  bench("constantTimeEq (native)", () => {
    mod.constantTimeEq(secretA, secretB);
  });

  bench("crypto.timingSafeEqual (Node.js)", () => {
    crypto.timingSafeEqual(secretA, secretB);
  });

  // --- detect_mime ---
  const pngBuffer = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00]);
  const jpegBuffer = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46]);

  bench("detectMime PNG (native)", () => {
    mod.detectMime(pngBuffer);
  });

  bench("detectMime JPEG (native)", () => {
    mod.detectMime(jpegBuffer);
  });
});
