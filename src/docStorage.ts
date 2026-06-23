export const DOC_STORAGE_KEY = "andromeda.doc";

export function readStoredDoc(): string {
  try {
    return localStorage.getItem(DOC_STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}
