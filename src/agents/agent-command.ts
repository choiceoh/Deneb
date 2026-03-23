// Barrel: re-exports the public API so existing imports continue to work.
// Implementation is split across:
//   agent-command-helpers.ts  — session/text utilities
//   agent-command-prepare.ts  — execution setup and validation
//   agent-command-run.ts      — per-attempt runner (CLI + Pi embedded)
//   agent-command-acp.ts      — ACP turn execution
//   agent-command-execute.ts  — main orchestration + public entrypoints
export { agentCommand, agentCommandFromIngress } from "./agent-command-execute.js";
