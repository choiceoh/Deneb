import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Port 1420 is the Tauri dev-server convention; harmless for plain `vite` too.
export default defineConfig({
  plugins: [react()],
  server: { port: 1420, strictPort: false },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
