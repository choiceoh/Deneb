import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 1420,
    proxy: {
      "/ws": {
        target: "ws://127.0.0.1:3710",
        ws: true,
      },
      "/files": {
        target: "http://127.0.0.1:3710",
      },
      "/health": {
        target: "http://127.0.0.1:3710",
      },
    },
  },
});
