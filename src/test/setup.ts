// Vitest global setup — jest-dom matchers (toBeInTheDocument, etc.) for all tests.
import "@testing-library/jest-dom/vitest";

// Node ≥22 ships an experimental built-in Web Storage API. On Node 25 it is enabled
// by default, but without `--localstorage-file` the global `localStorage` is a
// non-functional stub (its `.clear()`/`.getItem()` are undefined) that *shadows*
// jsdom's implementation — breaking every test that touches Web Storage with
// "localStorage.clear is not a function". Install a real in-memory Storage so the
// suite behaves identically on Node 22 (CI) and Node 25 (local dev).
function createMemoryStorage(): Storage {
  const store = new Map<string, string>();
  return {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key: string) {
      return store.get(key) ?? null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, String(value));
    },
  } as Storage;
}

for (const name of ["localStorage", "sessionStorage"] as const) {
  const storage = createMemoryStorage();
  Object.defineProperty(globalThis, name, { value: storage, configurable: true, writable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, name, { value: storage, configurable: true, writable: true });
  }
}
