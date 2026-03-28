<script lang="ts">
  import { app } from "$lib/state.svelte";

  let inputText = $state("");
  let textarea: HTMLTextAreaElement;

  function handleSend() {
    const text = inputText.trim();
    if (!text || app.isStreaming) return;
    app.sendMessage(text);
    inputText = "";
    if (textarea) textarea.style.height = "auto";
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
    if (e.key === "Escape" && app.isStreaming) {
      e.preventDefault();
      app.stopGeneration();
    }
  }

  function handleInput() {
    if (!textarea) return;
    textarea.style.height = "auto";
    textarea.style.height = Math.min(textarea.scrollHeight, 160) + "px";
  }
</script>

<div class="input-area">
  <div class="input-wrapper">
    <textarea
      id="chat-input"
      bind:this={textarea}
      bind:value={inputText}
      onkeydown={handleKeydown}
      oninput={handleInput}
      placeholder={app.isStreaming ? "응답 생성 중..." : "메시지를 입력하세요..."}
      disabled={app.isStreaming}
      rows="1"
    ></textarea>

    {#if app.isStreaming}
      <button class="action-btn stop" onclick={() => app.stopGeneration()} title="중지 (Esc)">
        <span class="stop-icon">■</span>
      </button>
    {:else}
      <button
        class="action-btn send"
        onclick={handleSend}
        disabled={!inputText.trim()}
        title="전송 (Enter)"
      >
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 2l-1.06 1.06L11.44 7.5H2v1.5h9.44l-4.5 4.44L8 14.5l6.5-6.5z" />
        </svg>
      </button>
    {/if}
  </div>

  <span class="input-hint">
    {app.isStreaming ? "Esc로 중지" : "Enter로 전송 · Shift+Enter로 줄바꿈"}
  </span>
</div>

<style>
  .input-area {
    padding: var(--space-md) var(--space-lg);
    padding-bottom: var(--space-sm);
    background: var(--bg-primary);
    border-top: 1px solid var(--bg-surface);
    flex-shrink: 0;
  }

  .input-wrapper {
    display: flex;
    align-items: flex-end;
    gap: var(--space-sm);
    background: var(--bg-secondary);
    border: 1px solid var(--bg-surface);
    border-radius: var(--radius-lg);
    padding: var(--space-sm);
    transition: border-color var(--transition-fast);
  }

  .input-wrapper:focus-within {
    border-color: var(--accent-primary);
  }

  textarea {
    flex: 1;
    resize: none;
    padding: 8px 12px;
    font-size: 14px;
    line-height: 1.5;
    color: var(--text-primary);
    background: transparent;
    min-height: 24px;
    max-height: 160px;
  }

  textarea::placeholder {
    color: var(--text-dim);
  }

  textarea:disabled {
    opacity: 0.5;
  }

  .action-btn {
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
    transition: all var(--transition-fast);
  }

  .action-btn.send {
    background: var(--accent-gradient);
    color: white;
  }

  .action-btn.send:hover:not(:disabled) {
    opacity: 0.9;
  }

  .action-btn.send:disabled {
    opacity: 0.3;
    cursor: default;
  }

  .action-btn.stop {
    background: rgba(247, 118, 142, 0.15);
    color: var(--error);
  }

  .action-btn.stop:hover {
    background: var(--error);
    color: white;
  }

  .stop-icon {
    font-size: 12px;
  }

  .input-hint {
    display: block;
    padding: var(--space-xs) var(--space-xs) 0;
    color: var(--text-dim);
    font-size: 11px;
  }
</style>
