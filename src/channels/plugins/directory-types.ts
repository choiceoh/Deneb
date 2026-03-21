import type { DenebConfig } from "../../config/types.js";

export type DirectoryConfigParams = {
  cfg: DenebConfig;
  accountId?: string | null;
  query?: string | null;
  limit?: number | null;
};
