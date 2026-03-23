import fs from "node:fs";
import path from "node:path";
import { resolveOAuthDir } from "../config/paths.js";
import { logVerbose, shouldLogVerbose } from "../globals.js";
import { resolveUserPath } from "./paths.js";
import { CONFIG_DIR } from "./paths.js";

export function normalizeE164(number: string): string {
  const withoutPrefix = number.replace(/^whatsapp:/, "").trim();
  const digits = withoutPrefix.replace(/[^\d+]/g, "");
  if (digits.startsWith("+")) {
    return `+${digits.slice(1)}`;
  }
  return `+${digits}`;
}

export type WebChannel = "web";

export function assertWebChannel(input: string): asserts input is WebChannel {
  if (input !== "web") {
    throw new Error("Web channel must be 'web'");
  }
}

export function isSelfChatMode(
  selfE164: string | null | undefined,
  allowFrom?: Array<string | number> | null,
): boolean {
  if (!selfE164) {
    return false;
  }
  if (!Array.isArray(allowFrom) || allowFrom.length === 0) {
    return false;
  }
  const normalizedSelf = normalizeE164(selfE164);
  return allowFrom.some((n) => {
    if (n === "*") {
      return false;
    }
    try {
      return normalizeE164(String(n)) === normalizedSelf;
    } catch {
      return false;
    }
  });
}

export function toWhatsappJid(number: string): string {
  const withoutPrefix = number.replace(/^whatsapp:/, "").trim();
  if (withoutPrefix.includes("@")) {
    return withoutPrefix;
  }
  const e164 = normalizeE164(withoutPrefix);
  const digits = e164.replace(/\D/g, "");
  return `${digits}@s.whatsapp.net`;
}

export type JidToE164Options = {
  authDir?: string;
  lidMappingDirs?: string[];
  logMissing?: boolean;
};

type LidLookup = {
  getPNForLID?: (jid: string) => Promise<string | null>;
};

function resolveLidMappingDirs(opts?: JidToE164Options): string[] {
  const dirs = new Set<string>();
  const addDir = (dir?: string | null) => {
    if (!dir) {
      return;
    }
    dirs.add(resolveUserPath(dir));
  };
  addDir(opts?.authDir);
  for (const dir of opts?.lidMappingDirs ?? []) {
    addDir(dir);
  }
  addDir(resolveOAuthDir());
  addDir(path.join(CONFIG_DIR, "credentials"));
  return [...dirs];
}

function readLidReverseMapping(lid: string, opts?: JidToE164Options): string | null {
  const mappingFilename = `lid-mapping-${lid}_reverse.json`;
  const mappingDirs = resolveLidMappingDirs(opts);
  for (const dir of mappingDirs) {
    const mappingPath = path.join(dir, mappingFilename);
    try {
      const data = fs.readFileSync(mappingPath, "utf8");
      const phone = JSON.parse(data) as string | number | null;
      if (phone === null || phone === undefined) {
        continue;
      }
      return normalizeE164(String(phone));
    } catch {
      // Try the next location.
    }
  }
  return null;
}

export function jidToE164(jid: string, opts?: JidToE164Options): string | null {
  const match = jid.match(/^(\d+)(?::\d+)?@(s\.whatsapp\.net|hosted)$/);
  if (match) {
    const digits = match[1];
    return `+${digits}`;
  }

  const lidMatch = jid.match(/^(\d+)(?::\d+)?@(lid|hosted\.lid)$/);
  if (lidMatch) {
    const lid = lidMatch[1];
    const phone = readLidReverseMapping(lid, opts);
    if (phone) {
      return phone;
    }
    const shouldLog = opts?.logMissing ?? shouldLogVerbose();
    if (shouldLog) {
      logVerbose(`LID mapping not found for ${lid}; skipping inbound message`);
    }
  }

  return null;
}

export async function resolveJidToE164(
  jid: string | null | undefined,
  opts?: JidToE164Options & { lidLookup?: LidLookup },
): Promise<string | null> {
  if (!jid) {
    return null;
  }
  const direct = jidToE164(jid, opts);
  if (direct) {
    return direct;
  }
  if (!/(@lid|@hosted\.lid)$/.test(jid)) {
    return null;
  }
  if (!opts?.lidLookup?.getPNForLID) {
    return null;
  }
  try {
    const pnJid = await opts.lidLookup.getPNForLID(jid);
    if (!pnJid) {
      return null;
    }
    return jidToE164(pnJid, opts);
  } catch (err) {
    if (shouldLogVerbose()) {
      logVerbose(`LID mapping lookup failed for ${jid}: ${String(err)}`);
    }
    return null;
  }
}
