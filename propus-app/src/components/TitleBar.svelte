<script lang="ts">
  import { app } from "$lib/state.svelte";
</script>

<header class="titlebar">
  <div class="titlebar-left">
    <button class="sidebar-toggle" onclick={() => app.toggleSidebar()} title="사이드바 토글">
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <rect y="2" width="16" height="1.5" rx="0.75" />
        <rect y="7" width="16" height="1.5" rx="0.75" />
        <rect y="12" width="16" height="1.5" rx="0.75" />
      </svg>
    </button>

    <span class="brand">PROPUS</span>
    <span class="version">v0.1</span>
  </div>

  <div class="titlebar-right">
    {#if app.serviceName}
      <span class="model-info">{app.serviceName} · {app.modelName}</span>
    {/if}

    <div class="status-group">
      {#if app.isStreaming}
        <span class="dot streaming"></span>
      {:else}
        <span class="dot" class:connected={app.connectionStatus === "connected"}></span>
      {/if}
      <span
        class="status-text"
        class:streaming={app.isStreaming}
        class:connected={!app.isStreaming && app.connectionStatus === "connected"}
        class:error={!app.isStreaming && app.connectionStatus !== "connected"}
      >
        {app.statusText}
      </span>
    </div>
  </div>
</header>

<style>
  .titlebar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 48px;
    padding: 0 var(--space-md);
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--bg-surface);
    flex-shrink: 0;
    /* web — no window drag */
  }

  .titlebar-left,
  .titlebar-right {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    /* web — no drag region needed */
  }

  .sidebar-toggle {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    transition: all var(--transition-fast);
  }

  .sidebar-toggle:hover {
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .brand {
    font-size: 16px;
    font-weight: 800;
    background: var(--accent-gradient);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .version {
    color: var(--text-dim);
    font-size: 11px;
  }

  .model-info {
    color: var(--text-muted);
    font-size: 12px;
  }

  .status-group {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--error);
    flex-shrink: 0;
  }

  .dot.connected {
    background: var(--success);
  }

  .dot.streaming {
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

  .status-text {
    font-size: 12px;
    color: var(--error);
  }

  .status-text.connected {
    color: var(--success);
  }

  .status-text.streaming {
    color: var(--warning);
  }
</style>
