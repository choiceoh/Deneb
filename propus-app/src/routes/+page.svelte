<script lang="ts">
  import { onMount, tick } from "svelte";
  import { app } from "$lib/state.svelte";
  import ConnectScreen from "$components/ConnectScreen.svelte";
  import TitleBar from "$components/TitleBar.svelte";
  import Sidebar from "$components/Sidebar.svelte";
  import ChatMessage from "$components/ChatMessage.svelte";
  import StreamingResponse from "$components/StreamingResponse.svelte";
  import InputArea from "$components/InputArea.svelte";

  let chatContainer: HTMLDivElement | undefined = $state();
  let isNearBottom = $state(true);
  let showScrollButton = $state(false);

  // Auto-scroll only when user is near the bottom (within 80px).
  $effect(() => {
    void app.messages.length;
    void app.streamingText;

    tick().then(() => {
      if (chatContainer && isNearBottom) {
        chatContainer.scrollTop = chatContainer.scrollHeight;
      }
      if (chatContainer && !isNearBottom && app.messages.length > 0) {
        showScrollButton = true;
      }
    });
  });

  function handleScroll() {
    if (!chatContainer) return;
    const distFromBottom =
      chatContainer.scrollHeight - chatContainer.scrollTop - chatContainer.clientHeight;
    isNearBottom = distFromBottom < 80;
    if (isNearBottom) showScrollButton = false;
  }

  function scrollToBottom() {
    if (chatContainer) {
      chatContainer.scrollTop = chatContainer.scrollHeight;
      isNearBottom = true;
      showScrollButton = false;
    }
  }

  function handleGlobalKeydown(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey;
    if (!mod) return;

    switch (e.key) {
      case "n":
        e.preventDefault();
        app.clearChat();
        break;
      case "s":
        e.preventDefault();
        app.saveSession();
        break;
      case "\\":
        e.preventDefault();
        app.toggleSidebar();
        break;
      case "l":
        e.preventDefault();
        document.getElementById("chat-input")?.focus();
        break;
    }
  }

  onMount(() => {
    app.initAutoConnect();
  });

  const suggestions = [
    { icon: "📂", label: "프로젝트 구조 분석", prompt: "이 프로젝트의 구조를 분석해줘" },
    { icon: "📖", label: "README 요약", prompt: "README.md를 읽고 요약해줘" },
    { icon: "🔍", label: "코드 리뷰", prompt: "최근 변경사항을 리뷰해줘" },
    { icon: "🧪", label: "테스트 실행", prompt: "테스트를 실행하고 결과를 알려줘" },
  ];
</script>

<svelte:window onkeydown={handleGlobalKeydown} />

{#if app.needsServerUrl || app.needsApiKey}
  <ConnectScreen />
{:else}
  <div class="app-layout">
    <TitleBar />

    {#if app.connectionStatus !== "connected"}
      <div class="reconnect-banner">
        <div class="reconnect-left">
          <span class="reconnect-dot">●</span>
          <span>서버 연결이 끊겼습니다</span>
        </div>
        <button class="reconnect-btn" onclick={() => app.reconnect()}>재연결</button>
      </div>
    {/if}

    <div class="main-area">
      <Sidebar />

      <div class="chat-area">
        <div class="chat-scroll" bind:this={chatContainer} onscroll={handleScroll}>
          {#if app.messages.length === 0 && !app.streamingText && !app.isStreaming}
            <div class="welcome">
              <h1 class="welcome-title">PROPUS</h1>
              <p class="welcome-sub">무엇을 도와드릴까요?</p>

              <div class="welcome-cards">
                {#each suggestions as s}
                  <button class="welcome-card" onclick={() => app.sendMessage(s.prompt)}>
                    <span class="card-icon">{s.icon}</span>
                    <span class="card-label">{s.label}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          <div class="messages">
            {#each app.messages as msg (msg.id)}
              <ChatMessage message={msg} />
            {/each}

            <StreamingResponse />
          </div>
        </div>

        {#if showScrollButton}
          <button class="scroll-bottom-btn" onclick={scrollToBottom} title="최신 메시지로">
            ↓
          </button>
        {/if}

        <InputArea />
      </div>
    </div>
  </div>
{/if}

<style>
  .app-layout {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }

  .reconnect-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 20px;
    background: rgba(90, 32, 32, 0.6);
    flex-shrink: 0;
  }

  .reconnect-left {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    color: var(--error);
    font-size: 13px;
  }

  .reconnect-dot {
    font-size: 10px;
  }

  .reconnect-btn {
    padding: 6px 16px;
    background: rgba(247, 118, 142, 0.15);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    font-size: 12px;
    font-weight: 600;
    transition: background var(--transition-fast);
  }

  .reconnect-btn:hover {
    background: var(--error);
  }

  .main-area {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .chat-area {
    display: flex;
    flex-direction: column;
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .chat-scroll {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-lg) 40px;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: var(--space-sm);
  }

  .welcome {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding-top: 100px;
    gap: var(--space-md);
  }

  .welcome-title {
    font-size: 52px;
    font-weight: 800;
    background: var(--accent-gradient);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
    letter-spacing: -2px;
  }

  .welcome-sub {
    color: var(--text-muted);
    font-size: 16px;
  }

  .welcome-cards {
    display: flex;
    gap: var(--space-md);
    margin-top: var(--space-xl);
    flex-wrap: wrap;
    justify-content: center;
  }

  .welcome-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 14px;
    width: 180px;
    background: var(--bg-tertiary);
    border: 1px solid var(--bg-surface);
    border-radius: var(--radius-md);
    text-align: left;
    transition: all var(--transition-fast);
  }

  .welcome-card:hover {
    border-color: var(--accent-primary);
    background: rgba(122, 162, 247, 0.08);
  }

  .card-icon {
    font-size: 18px;
  }

  .card-label {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .welcome-card:hover .card-label {
    color: var(--text-primary);
  }

  .scroll-bottom-btn {
    position: absolute;
    bottom: 90px;
    right: 24px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--bg-tertiary);
    border: 1px solid var(--bg-surface);
    color: var(--text-secondary);
    font-size: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all var(--transition-fast);
    z-index: 10;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  }

  .scroll-bottom-btn:hover {
    background: var(--accent-primary);
    color: white;
    border-color: var(--accent-primary);
  }
</style>
