<script lang="ts">
  import type { ChatMessage as ChatMsg } from "$lib/types";
  import CodeBlock from "./CodeBlock.svelte";
  import ToolMessage from "./ToolMessage.svelte";

  let { message }: { message: ChatMsg } = $props();
</script>

{#if message.role === "tool"}
  <ToolMessage {message} />
{:else}
  <div class="chat-msg" class:user={message.role === "user"}>
    <!-- Avatar + Name -->
    <div class="msg-header">
      <div class="avatar" class:user-avatar={message.role === "user"}>
        {message.role === "user" ? "U" : "P"}
      </div>
      <span class="msg-author" class:user-name={message.role === "user"}>
        {message.role === "user" ? "You" : "Propus"}
      </span>
    </div>

    <!-- Content with segments -->
    <div class="msg-content">
      {#each message.segments as seg}
        {#if seg.type === "code"}
          <CodeBlock code={seg.content} language={seg.language} />
        {:else}
          <p class="text-segment">{seg.content}</p>
        {/if}
      {/each}
    </div>
  </div>
{/if}

<style>
  .chat-msg {
    padding: 14px 16px;
    border-radius: var(--radius-md);
    transition: background var(--transition-fast);
  }

  .chat-msg.user {
    background: var(--bg-secondary);
    border: 1px solid var(--bg-surface);
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
    flex-shrink: 0;
    background: #2d3250;
    color: #7dcfff;
  }

  .avatar.user-avatar {
    background: #3d59a1;
    color: var(--text-primary);
  }

  .msg-author {
    font-size: 14px;
    font-weight: 600;
    color: #7dcfff;
  }

  .msg-author.user-name {
    color: var(--text-primary);
  }

  .msg-content {
    padding-left: 38px; /* align with text after avatar */
  }

  .text-segment {
    color: var(--text-primary);
    font-size: 14px;
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
  }

  .text-segment + .text-segment {
    margin-top: var(--space-sm);
  }
</style>
