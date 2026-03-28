<script lang="ts">
  import type { ChatMessage } from "$lib/types";

  let { message }: { message: ChatMessage } = $props();
  let expanded = $state(message.expanded ?? false);

  function toggle() {
    expanded = !expanded;
  }

  // Truncate tool result for display.
  function truncate(text: string, max: number): string {
    if (text.length <= max) return text;
    const lines = text.split("\n");
    if (lines.length > 8) {
      return lines.slice(0, 6).join("\n") + `\n... (${lines.length - 6} more lines)`;
    }
    return text.slice(0, max) + "...";
  }

  // Map tool names to icons for visual clarity.
  const toolIcons: Record<string, string> = {
    read: "📄",
    write: "✏️",
    edit: "📝",
    exec: "⚙️",
    grep: "🔍",
    find: "📁",
    ls: "📂",
    web: "🌐",
    web_search: "🌐",
    http: "🌐",
  };

  function getToolIcon(name?: string): string {
    if (!name) return "🔧";
    // Strip " ✓" suffix for lookup.
    const base = name.replace(/ ✓$/, "").toLowerCase();
    return toolIcons[base] ?? "🔧";
  }

  function formatDuration(ms?: number): string {
    if (ms == null) return "";
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }
</script>

<div class="tool-msg">
  <button class="tool-header" onclick={toggle}>
    <span class="tool-icon">{getToolIcon(message.toolName)}</span>
    <span class="tool-name">{message.toolName}</span>
    {#if message.toolDuration}
      <span class="tool-duration">{formatDuration(message.toolDuration)}</span>
    {/if}
    <span class="tool-chevron">{expanded ? "▼" : "▶"}</span>
  </button>

  {#if expanded}
    <div class="tool-body">
      {#if message.content}
        <pre class="tool-content">{truncate(message.content, 800)}</pre>
      {/if}
      {#if message.toolResult}
        <pre class="tool-content">{truncate(message.toolResult, 800)}</pre>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool-msg {
    background: rgba(26, 30, 48, 0.6);
    border-radius: var(--radius-sm);
    border: 1px solid var(--bg-surface);
    overflow: hidden;
  }

  .tool-header {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    padding: 8px 12px;
    width: 100%;
    text-align: left;
    transition: background var(--transition-fast);
  }

  .tool-header:hover {
    background: rgba(45, 50, 32, 0.3);
  }

  .tool-icon {
    font-size: 14px;
    flex-shrink: 0;
    width: 22px;
    text-align: center;
  }

  .tool-name {
    color: var(--tool-text);
    font-size: 12px;
    font-weight: 600;
  }

  .tool-duration {
    color: var(--text-muted);
    font-size: 11px;
    font-family: var(--font-mono);
  }

  .tool-chevron {
    color: var(--text-muted);
    font-size: 10px;
    margin-left: auto;
  }

  .tool-body {
    border-top: 1px solid var(--bg-surface);
    padding: 10px 12px;
  }

  .tool-content {
    color: var(--text-muted);
    font-size: 11px;
    font-family: var(--font-mono);
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
    max-height: 200px;
    overflow-y: auto;
  }
</style>
