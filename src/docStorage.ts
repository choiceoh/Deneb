import { getString } from "./storage";

export const DOC_STORAGE_KEY = "andromeda.doc";

export function readStoredDoc(): string {
  return getString(DOC_STORAGE_KEY);
}
