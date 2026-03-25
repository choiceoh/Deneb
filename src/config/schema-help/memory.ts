export const MEMORY_HELP: Record<string, string> = {
  memory: "Memory backend configuration (global).",
  "memory.backend":
    'Selects the global memory engine: "builtin" uses Deneb memory internals, while "vega" uses the Vega subprocess backend.',
  "memory.citations":
    'Controls citation visibility in replies: "auto" shows citations when useful, "on" always shows them, and "off" hides them. Keep "auto" for a balanced signal-to-noise default.',
};
