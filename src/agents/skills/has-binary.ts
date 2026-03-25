import fs from "node:fs";
import path from "node:path";

const hasBinaryCache = new Map<string, boolean>();

export function hasBinary(bin: string): boolean {
  if (hasBinaryCache.has(bin)) {
    return hasBinaryCache.get(bin)!;
  }
  const pathEnv = process.env.PATH ?? "";
  const parts = pathEnv.split(path.delimiter).filter(Boolean);
  const extensions =
    process.platform === "win32"
      ? [
          "",
          ...(process.env.PATHEXT ?? ".EXE;.CMD;.BAT;.COM")
            .split(";")
            .map((v) => v.trim())
            .filter(Boolean),
        ]
      : [""];
  for (const part of parts) {
    for (const ext of extensions) {
      const candidate = path.join(part, bin + ext);
      try {
        fs.accessSync(candidate, fs.constants.X_OK);
        hasBinaryCache.set(bin, true);
        return true;
      } catch {
        // keep scanning
      }
    }
  }
  hasBinaryCache.set(bin, false);
  return false;
}
