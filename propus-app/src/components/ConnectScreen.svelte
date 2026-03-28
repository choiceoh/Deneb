<script lang="ts">
  import { app } from "$lib/state.svelte";
  import { deriveWsUrl, saveUrl } from "$lib/ws";

  let serverUrl = $state("");
  let apiKey = $state("");
  let showManualUrl = $state(false);

  function handleConnect() {
    const url = serverUrl.trim();
    if (url) {
      saveUrl(url);
      app.connectToServer(url);
    }
  }

  function handleAutoConnect() {
    app.connectToServer(deriveWsUrl());
  }

  function handleApiKey() {
    if (apiKey.trim()) app.submitApiKey(apiKey.trim());
  }

  function handleKeydown(e: KeyboardEvent, action: () => void) {
    if (e.key === "Enter") action();
  }
</script>

<div class="connect-screen">
  <div class="connect-card">
    <div class="logo">
      <span class="logo-text">PROPUS</span>
      <span class="logo-sub">AI Coding Assistant</span>
    </div>

    {#if app.needsApiKey}
      <div class="form-section">
        <h2>API 키 설정</h2>
        <p class="hint">Z.AI 코딩플랜 API 키를 입력하세요.</p>
        <input
          type="password"
          bind:value={apiKey}
          placeholder="API 키를 붙여넣으세요..."
          onkeydown={(e) => handleKeydown(e, handleApiKey)}
        />
        <button class="primary-btn" onclick={handleApiKey}>시작하기</button>
      </div>
    {:else}
      <div class="form-section">
        <h2>서버 연결</h2>
        <p class="hint">게이트웨이에 연결합니다.</p>
        <button class="primary-btn" onclick={handleAutoConnect}>연결</button>

        {#if !showManualUrl}
          <button class="link-btn" onclick={() => (showManualUrl = true)}>
            다른 서버에 연결...
          </button>
        {:else}
          <input
            type="text"
            bind:value={serverUrl}
            placeholder="ws://192.168.1.100:3710/ws"
            onkeydown={(e) => handleKeydown(e, handleConnect)}
          />
          <button class="secondary-btn" onclick={handleConnect}>수동 연결</button>
        {/if}
      </div>
    {/if}

    <span class="status">{app.statusText}</span>
  </div>
</div>

<style>
  .connect-screen {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100vh;
    background: var(--bg-primary);
  }

  .connect-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-lg);
    max-width: 420px;
    width: 100%;
    padding: var(--space-xl);
  }

  .logo {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-xs);
  }

  .logo-text {
    font-size: 48px;
    font-weight: 800;
    background: var(--accent-gradient);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
    letter-spacing: -1px;
  }

  .logo-sub {
    color: var(--text-muted);
    font-size: 14px;
  }

  .form-section {
    width: 100%;
    background: var(--bg-tertiary);
    border-radius: var(--radius-lg);
    border: 1px solid var(--bg-surface);
    padding: var(--space-lg);
    display: flex;
    flex-direction: column;
    gap: var(--space-md);
  }

  .form-section h2 {
    color: var(--text-primary);
    font-size: 18px;
    font-weight: 700;
  }

  .hint {
    color: var(--text-muted);
    font-size: 13px;
    line-height: 1.5;
  }

  .form-section input {
    width: 100%;
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--bg-surface);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: 14px;
    transition: border-color var(--transition-fast);
  }

  .form-section input:focus {
    border-color: var(--accent-primary);
  }

  .form-section input::placeholder {
    color: var(--text-dim);
  }

  .primary-btn {
    width: 100%;
    padding: 12px;
    background: var(--accent-gradient);
    border-radius: var(--radius-md);
    color: white;
    font-size: 14px;
    font-weight: 600;
    transition: opacity var(--transition-fast);
  }

  .primary-btn:hover {
    opacity: 0.9;
  }

  .secondary-btn {
    width: 100%;
    padding: 10px;
    background: var(--bg-surface);
    border-radius: var(--radius-md);
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 600;
    transition: background var(--transition-fast);
  }

  .secondary-btn:hover {
    background: rgba(122, 162, 247, 0.15);
  }

  .link-btn {
    color: var(--text-muted);
    font-size: 12px;
    text-align: center;
    transition: color var(--transition-fast);
  }

  .link-btn:hover {
    color: var(--accent-primary);
  }

  .status {
    color: var(--text-dim);
    font-size: 11px;
  }
</style>
