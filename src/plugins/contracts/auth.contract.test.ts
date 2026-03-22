import { describe, it } from "vitest";

// Skipped: provider extensions (github-copilot, openai, qwen-portal-auth) are not bundled in this repo.
describe.skip("provider auth contract (extensions not bundled)", () => {
  it.skip("keeps OpenAI Codex OAuth auth results provider-owned", () => {});
  it.skip("keeps OpenAI Codex OAuth failures non-fatal at the provider layer", () => {});
  it.skip("keeps Qwen portal OAuth auth results provider-owned", () => {});
  it.skip("keeps GitHub Copilot device auth results provider-owned", () => {});
  it.skip("keeps GitHub Copilot auth gated on interactive TTYs", () => {});
});
