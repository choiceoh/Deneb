<script lang="ts">
  import { app } from "$lib/state.svelte";

  const suggestions = [
    { icon: "📂", label: "프로젝트 구조 분석", prompt: "이 프로젝트의 구조를 분석해줘" },
    { icon: "📖", label: "README 요약", prompt: "README.md를 읽고 요약해줘" },
    { icon: "🔍", label: "코드 리뷰", prompt: "최근 변경사항을 리뷰해줘" },
    { icon: "🧪", label: "테스트 실행", prompt: "테스트를 실행하고 결과를 알려줘" },
  ];

  let updateStatus = $state("");
  let updating = $state(false);

  async function checkForUpdate() {
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      const result = await invoke("check_update");
      updateStatus = result || "이미 최신 버전입니다";
    } catch (e: any) {
      updateStatus = "확인 실패: " + e;
    }
  }

  async function doUpdate() {
    updating = true;
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      await invoke("install_update");
      updateStatus = "업데이트 완료! 재시작 중...";
    } catch (e: any) {
      updateStatus = "업데이트 실패: " + e;
    }
    updating = false;
  }
</script>

{#if app.sidebarVisible}
  <aside class="sidebar">
    <div class="sidebar-content">
      <button class="new-chat-btn" onclick={() => app.clearChat()}>
        <span class="plus">+</span>
        <span>새 대화</span>
      </button>

      <div class="section">
        <span class="section-label">빠른 시작</span>
        <div class="suggestions">
          {#each suggestions as s}
            <button class="suggestion" onclick={() => app.sendMessage(s.prompt)}>
              <span class="suggestion-icon">{s.icon}</span>
              <span class="suggestion-label">{s.label}</span>
            </button>
          {/each}
        </div>
      </div>

      <div class="section">
        <span class="section-label">작업</span>
        <button class="action-btn" onclick={() => app.saveSession()}>세션 저장</button>
        <button class="action-btn" onclick={checkForUpdate}>🔄 업데이트 확인</button>
        {#if updateStatus}
          <div class="update-info">
            <span class="update-text">{updateStatus}</span>
            {#if updateStatus.includes("→")}
              <button class="update-btn" onclick={doUpdate} disabled={updating}>
                {updating ? "설치 중..." : "업데이트 설치"}
              </button>
            {/if}
          </div>
        {/if}
      </div>

      <div class="spacer"></div>

      <div class="stats">
        {#if app.usageText}
          <span class="stat-line">{app.usageText}</span>
        {/if}
        {#if app.msgCount > 0}
          <span class="stat-line">메시지 {app.msgCount}</span>
        {/if}
      </div>
    </div>
  </aside>
{/if}

<style>
  .sidebar {
    width: 240px;
    flex-shrink: 0;
    background: var(--sidebar-bg);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border-right: 1px solid var(--sidebar-border);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .sidebar-content {
    display: flex;
    flex-direction: column;
    padding: var(--space-md);
    gap: var(--space-md);
    height: 100%;
    overflow-y: auto;
  }

  .new-chat-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-sm);
    padding: 10px;
    background: var(--accent-gradient);
    border-radius: var(--radius-md);
    color: white;
    font-size: 13px;
    font-weight: 600;
    transition: opacity var(--transition-fast);
  }

  .new-chat-btn:hover {
    opacity: 0.9;
  }

  .plus {
    font-size: 18px;
    font-weight: 700;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-sm);
  }

  .section-label {
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
    padding-left: 4px;
  }

  .suggestions {
    display: flex;
    flex-direction: column;
    gap: var(--space-xs);
  }

  .suggestion {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    text-align: left;
    transition: background var(--transition-fast);
  }

  .suggestion:hover {
    background: rgba(122, 162, 247, 0.1);
  }

  .suggestion-icon {
    font-size: 14px;
    flex-shrink: 0;
  }

  .suggestion-label {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .suggestion:hover .suggestion-label {
    color: var(--text-primary);
  }

  .action-btn {
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    font-size: 13px;
    text-align: left;
    transition: all var(--transition-fast);
  }

  .action-btn:hover {
    background: rgba(122, 162, 247, 0.1);
    color: var(--text-primary);
  }

  .spacer {
    flex: 1;
  }

  .stats {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-top: 1px solid var(--bg-surface);
    padding-top: var(--space-sm);
  }

  .stat-line {
    color: var(--text-dim);
    font-size: 11px;
  }

  .update-info {
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    background: rgba(122, 162, 247, 0.08);
  }

  .update-text {
    font-size: 11px;
    color: var(--text-secondary);
    display: block;
    margin-bottom: 6px;
  }

  .update-btn {
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    font-size: 11px;
    background: var(--accent-gradient);
    color: white;
    font-weight: 600;
  }

  .update-btn:disabled {
    opacity: 0.5;
  }
</style>
