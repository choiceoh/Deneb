import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Surface the package version to the app (settings → 정보) as a build-time constant.
const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf8")) as { version: string };

// Port 1420 is the Tauri dev-server convention; harmless for plain `vite` too.
export default defineConfig({
  plugins: [react()],
  define: { __APP_VERSION__: JSON.stringify(pkg.version) },
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: { port: 1420, strictPort: false },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    // Ephemeral git worktrees (Claude Code sessions) live under .claude/ and carry
    // their own copies of these test files — don't double-run them from the root.
    exclude: [...configDefaults.exclude, "**/.claude/**"],
  },
});
