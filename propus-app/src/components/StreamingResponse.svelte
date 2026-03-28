<script lang="ts">
  import { app } from "$lib/state.svelte";
  import CodeBlock from "./CodeBlock.svelte";
  import MarkdownText from "./MarkdownText.svelte";
</script>

{#if app.streamingText || app.isStreaming}
  <div class="streaming-msg">
    <div class="msg-header">
      <div class="avatar">P</div>
      <span class="msg-author">Propus</span>
      {#if app.isStreaming}
        <span class="streaming-dot"></span>
      {/if}
    </div>

    <div class="msg-content">
      {#if app.streamingText}
        {#each app.streamingSegments as seg}
          {#if seg.type === "code"}
            <CodeBlock code={seg.content} language={seg.language} />
          {:else}
            <MarkdownText content={seg.content} />
          {/if}
        {/each}
      {:else if app.isStreaming}
        <p class="thinking">생각하는 중...</p>
      {/if}
    </div>
  </div>
{/if}

<style>
  .streaming-msg {
    padding: 14px 16px;
    border-radius: var(--radius-md);
  }

  .msg-header {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: var(--space-sm);
  }

  .avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    font-weight: 700;
    background: #2d3250;
    color: #7dcfff;
    flex-shrink: 0;
  }

  .msg-author {
    font-size: 14px;
    font-weight: 600;
    color: #7dcfff;
  }

  .streaming-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--warning);
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }

  .msg-content {
    padding-left: 38px;
  }

  .text-segment {
    color: var(--text-primary);
    font-size: 14px;
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
  }

  .thinking {
    color: var(--text-muted);
    font-size: 13px;
    margin: 0;
  }
</style>
