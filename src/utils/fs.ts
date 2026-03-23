import fs from "node:fs";

export async function ensureDir(dir: string) {
  await fs.promises.mkdir(dir, { recursive: true });
}

/**
 * Check if a file or directory exists at the given path.
 */
export async function pathExists(targetPath: string): Promise<boolean> {
  try {
    await fs.promises.access(targetPath);
    return true;
  } catch {
    return false;
  }
}
