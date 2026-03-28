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
</script>

<div class="tool-msg">
  <button class="tool-header" onclick={toggle}>
    <span class="tool-badge">T</span>
    <span class="tool-name">{message.toolName}</span>
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

  .tool-badge {
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 5px;
    background: var(--tool-bg);
    color: var(--tool-text);
    font-size: 10px;
    font-weight: 700;
    flex-shrink: 0;
  }

  .tool-name {
    color: var(--tool-text);
    font-size: 12px;
    font-weight: 600;
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
