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
  { ignores: ["dist", "node_modules", "src-tauri", "coverage", ".claude"] },
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
    },
  },
  {
    // Build tooling runs in Node (ESM). Give it Node globals so no-undef passes.
    files: ["scripts/**/*.{js,mjs}"],
    languageOptions: { globals: { ...globals.node } },
  },
  prettier,
);
