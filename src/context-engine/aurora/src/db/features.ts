import type { DatabaseSync } from "node:sqlite";

export type AuroraDbFeatures = {
  fts5Available: boolean;
};

const featureCache = new WeakMap<DatabaseSync, AuroraDbFeatures>();

function probeFts5(db: DatabaseSync): boolean {
  try {
    db.exec("DROP TABLE IF EXISTS temp.__aurora_fts5_probe");
    db.exec("CREATE VIRTUAL TABLE temp.__aurora_fts5_probe USING fts5(content)");
    db.exec("DROP TABLE temp.__aurora_fts5_probe");
    return true;
  } catch {
    try {
      db.exec("DROP TABLE IF EXISTS temp.__aurora_fts5_probe");
    } catch {
      // Ignore cleanup failures after a failed probe.
    }
    return false;
  }
}

/**
 * Detect SQLite features exposed by the current Node runtime.
 *
 * The result is cached per DatabaseSync handle because the probe is runtime-
 * specific, not database-file-specific.
 */
export function getAuroraDbFeatures(db: DatabaseSync): AuroraDbFeatures {
  const cached = featureCache.get(db);
  if (cached) {
    return cached;
  }

  const detected: AuroraDbFeatures = {
    fts5Available: probeFts5(db),
  };
  featureCache.set(db, detected);
  return detected;
}
