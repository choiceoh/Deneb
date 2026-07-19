import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import prettier from "eslint-config-prettier";
import globals from "globals";

// Flat config. Keep rules pragmatic: tsc already covers type errors, so ESLint
// focuses on correctness (hooks rules) and consistency. `any` is allowed at the
// dynamic RPC boundary (see dataProvider.ts).
export default tseslint.config(
  { ignores: ["dist", "node_modules", "src-tauri", "coverage", ".claude", "src/gen"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: { ...globals.browser, ...globals.node },
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      // Script dialogs are silent no-ops in the Tauri WebView (wry implements
      // none): window.confirm returns falsy without showing anything, so a
      // confirm() gate cancels itself in the desktop app while passing in
      // browser dev and jsdom tests (#3913/#3924 swept the codebase clean).
      // Use ConfirmModal / Modal instead.
      "no-restricted-globals": [
        "error",
        { name: "confirm", message: "Tauri WebView no-op — use ConfirmModal (@/components/ConfirmModal)." },
        { name: "alert", message: "Tauri WebView no-op — surface via pane error/status UI." },
        { name: "prompt", message: "Tauri WebView no-op — use a Modal with a Field input." },
      ],
      "no-restricted-properties": [
        "error",
        { object: "window", property: "confirm", message: "Tauri WebView no-op — use ConfirmModal." },
        { object: "window", property: "alert", message: "Tauri WebView no-op — surface via pane error/status UI." },
        { object: "window", property: "prompt", message: "Tauri WebView no-op — use a Modal with a Field input." },
      ],
    },
  },
  {
    // Build tooling runs in Node (ESM). Give it Node globals so no-undef passes.
    files: ["scripts/**/*.{js,mjs}"],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    // Test fixtures never hot-reload — mixing helper exports with wrapper
    // components is the point of a fixture module.
    files: ["src/test/**/*.{ts,tsx}", "**/*.test.{ts,tsx}"],
    rules: { "react-refresh/only-export-components": "off" },
  },
  prettier,
);
