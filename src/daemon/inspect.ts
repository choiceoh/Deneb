import fs from "node:fs/promises";
import path from "node:path";
import {
  GATEWAY_SERVICE_KIND,
  GATEWAY_SERVICE_MARKER,
  resolveGatewaySystemdServiceName,
} from "./constants.js";
import { resolveHomeDir } from "./paths.js";

export type ExtraGatewayService = {
  platform: "linux";
  label: string;
  detail: string;
  scope: "user" | "system";
  marker?: "deneb" | "clawdbot" | "moltbot";
  legacy?: boolean;
};

export type FindExtraGatewayServicesOptions = {
  deep?: boolean;
};

const EXTRA_MARKERS = ["deneb", "clawdbot", "moltbot"] as const;

export function renderGatewayServiceCleanupHints(
  env: Record<string, string | undefined> = process.env as Record<string, string | undefined>,
): string[] {
  const profile = env.DENEB_PROFILE;
  const unit = resolveGatewaySystemdServiceName(profile);
  return [
    `systemctl --user disable --now ${unit}.service`,
    `rm ~/.config/systemd/user/${unit}.service`,
  ];
}

type Marker = (typeof EXTRA_MARKERS)[number];

function detectMarker(content: string): Marker | null {
  const lower = content.toLowerCase();
  for (const marker of EXTRA_MARKERS) {
    if (lower.includes(marker)) {
      return marker;
    }
  }
  return null;
}

function hasGatewayServiceMarker(content: string): boolean {
  const lower = content.toLowerCase();
  const markerKeys = ["deneb_service_marker"];
  const kindKeys = ["deneb_service_kind"];
  const markerValues = [GATEWAY_SERVICE_MARKER.toLowerCase()];
  const hasMarkerKey = markerKeys.some((key) => lower.includes(key));
  const hasKindKey = kindKeys.some((key) => lower.includes(key));
  const hasMarkerValue = markerValues.some((value) => lower.includes(value));
  return (
    hasMarkerKey &&
    hasKindKey &&
    hasMarkerValue &&
    lower.includes(GATEWAY_SERVICE_KIND.toLowerCase())
  );
}

function isDenebGatewaySystemdService(name: string, contents: string): boolean {
  if (hasGatewayServiceMarker(contents)) {
    return true;
  }
  if (!name.startsWith("deneb-gateway")) {
    return false;
  }
  return contents.toLowerCase().includes("gateway");
}

function isIgnoredSystemdName(name: string): boolean {
  return name === resolveGatewaySystemdServiceName();
}

async function readDirEntries(dir: string): Promise<string[]> {
  try {
    return await fs.readdir(dir);
  } catch {
    return [];
  }
}

async function readUtf8File(filePath: string): Promise<string | null> {
  try {
    return await fs.readFile(filePath, "utf8");
  } catch {
    return null;
  }
}

type ServiceFileEntry = {
  entry: string;
  name: string;
  fullPath: string;
  contents: string;
};

async function collectServiceFiles(params: {
  dir: string;
  extension: string;
  isIgnoredName: (name: string) => boolean;
}): Promise<ServiceFileEntry[]> {
  const out: ServiceFileEntry[] = [];
  const entries = await readDirEntries(params.dir);
  for (const entry of entries) {
    if (!entry.endsWith(params.extension)) {
      continue;
    }
    const name = entry.slice(0, -params.extension.length);
    if (params.isIgnoredName(name)) {
      continue;
    }
    const fullPath = path.join(params.dir, entry);
    const contents = await readUtf8File(fullPath);
    if (contents === null) {
      continue;
    }
    out.push({ entry, name, fullPath, contents });
  }
  return out;
}

async function scanSystemdDir(params: {
  dir: string;
  scope: "user" | "system";
}): Promise<ExtraGatewayService[]> {
  const results: ExtraGatewayService[] = [];
  const candidates = await collectServiceFiles({
    dir: params.dir,
    extension: ".service",
    isIgnoredName: isIgnoredSystemdName,
  });

  for (const { entry, name, fullPath, contents } of candidates) {
    const marker = detectMarker(contents);
    if (!marker) {
      continue;
    }
    if (marker === "deneb" && isDenebGatewaySystemdService(name, contents)) {
      continue;
    }
    results.push({
      platform: "linux",
      label: entry,
      detail: `unit: ${fullPath}`,
      scope: params.scope,
      marker,
      legacy: marker !== "deneb",
    });
  }

  return results;
}

export async function findExtraGatewayServices(
  env: Record<string, string | undefined>,
  opts: FindExtraGatewayServicesOptions = {},
): Promise<ExtraGatewayService[]> {
  const results: ExtraGatewayService[] = [];
  const seen = new Set<string>();
  const push = (svc: ExtraGatewayService) => {
    const key = `${svc.platform}:${svc.label}:${svc.detail}:${svc.scope}`;
    if (seen.has(key)) {
      return;
    }
    seen.add(key);
    results.push(svc);
  };

  try {
    const home = resolveHomeDir(env);
    const userDir = path.join(home, ".config", "systemd", "user");
    for (const svc of await scanSystemdDir({
      dir: userDir,
      scope: "user",
    })) {
      push(svc);
    }
    if (opts.deep) {
      for (const dir of ["/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"]) {
        for (const svc of await scanSystemdDir({
          dir,
          scope: "system",
        })) {
          push(svc);
        }
      }
    }
  } catch {
    return results;
  }
  return results;
}
