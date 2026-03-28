<script lang="ts">
  import { app } from "$lib/state.svelte";
  import type { SessionPreview } from "$lib/types";

  let searchInput = $state("");
  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  let updateStatus = $state("");
  let updating = $state(false);

  // Group sessions by date.
  function groupByDate(sessions: SessionPreview[]): { label: string; items: SessionPreview[] }[] {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const yesterday = today - 86400000;
    const weekAgo = today - 7 * 86400000;

    const groups: Record<string, SessionPreview[]> = {};
    const order: string[] = [];

    for (const s of sessions) {
      let label: string;
      if (s.updated_at >= today) {
        label = "오늘";
      } else if (s.updated_at >= yesterday) {
        label = "어제";
      } else if (s.updated_at >= weekAgo) {
        label = "이번 주";
      } else {
        label = "이전";
      }
      if (!groups[label]) {
        groups[label] = [];
        order.push(label);
      }
      groups[label].push(s);
    }

    return order.map((label) => ({ label, items: groups[label] }));
  }

  function handleSearchInput(e: Event) {
    const value = (e.target as HTMLInputElement).value;
    searchInput = value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      app.searchSessions(value);
    }, 300);
  }

  function clearSearch() {
    searchInput = "";
    app.searchSessions("");
  }

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

  let sessionGroups = $derived(groupByDate(app.sessions));
</script>

{#if app.sidebarVisible}
  <aside class="sidebar">
    <div class="sidebar-content">
      <!-- New Chat button -->
      <button class="new-chat-btn" onclick={() => app.clearChat()}>
        <span class="plus">+</span>
        <span>새 대화</span>
      </button>

      <!-- Search -->
      <div class="search-wrapper">
        <svg class="search-icon" width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
          <path
            d="M11.742 10.344a6.5 6.5 0 1 0-1.397 1.398l3.85 3.85a1 1 0 0 0 1.415-1.414l-3.868-3.834zm-5.242.656a5 5 0 1 1 0-10 5 5 0 0 1 0 10z"
          />
        </svg>
        <input
          class="search-input"
          type="text"
          placeholder="검색..."
          value={searchInput}
          oninput={handleSearchInput}
        />
        {#if searchInput}
          <button class="search-clear" onclick={clearSearch}>×</button>
        {/if}
      </div>

      <!-- Session list -->
      <div class="session-list">
        {#if app.sessions.length === 0}
          <div class="empty-state">
            <span class="empty-text">세션 없음</span>
          </div>
        {:else}
          {#each sessionGroups as group}
            <div class="date-group">
              <span class="date-label">{group.label}</span>
              {#each group.items as session}
                <button
                  class="session-item"
                  class:active={session.key === app.currentSessionKey}
                  onclick={() => app.switchSession(session.key)}
                  title={session.title}
                >
                  <span
                    class="session-dot"
                    class:dot-active={session.status === "active"}
                    class:dot-done={session.status === "done"}
                  ></span>
                  <div class="session-info">
                    <span class="session-title">{session.title}</span>
                    <span class="session-meta">{session.message_count}개 메시지</span>
                  </div>
                </button>
              {/each}
            </div>
          {/each}
        {/if}
      </div>

      <!-- Bottom actions -->
      <div class="bottom-actions">
        {#if app.usageText}
          <span class="stat-line">{app.usageText}</span>
        {/if}
        <button class="action-btn" onclick={() => app.saveSession()}>세션 저장</button>
        <button class="action-btn" onclick={checkForUpdate}>업데이트 확인</button>
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
    </div>
  </aside>
{/if}

<style>
  .sidebar {
    width: 260px;
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
    height: 100%;
    overflow: hidden;
  }

  /* --- New Chat button --- */
  .new-chat-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-sm);
    padding: 10px;
    margin: var(--space-md) var(--space-md) 0;
    background: var(--accent-gradient);
    border-radius: var(--radius-md);
    color: white;
    font-size: 13px;
    font-weight: 600;
    transition: opacity var(--transition-fast);
    flex-shrink: 0;
  }

  .new-chat-btn:hover {
    opacity: 0.9;
  }

  .plus {
    font-size: 18px;
    font-weight: 700;
  }

  /* --- Search --- */
  .search-wrapper {
    position: relative;
    margin: var(--space-sm) var(--space-md);
    flex-shrink: 0;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-dim);
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: 7px 28px 7px 32px;
    border: 1px solid var(--bg-surface);
    border-radius: var(--radius-sm);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 12px;
    outline: none;
    transition: border-color var(--transition-fast);
  }

  .search-input::placeholder {
    color: var(--text-dim);
  }

  .search-input:focus {
    border-color: var(--accent-primary);
  }

  .search-clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    width: 18px;
    height: 18px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    color: var(--text-muted);
    font-size: 14px;
    line-height: 1;
  }

  .search-clear:hover {
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  /* --- Session list --- */
  .session-list {
    flex: 1;
    overflow-y: auto;
    padding: 0 var(--space-sm);
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-xl) 0;
  }

  .empty-text {
    color: var(--text-dim);
    font-size: 12px;
  }

  .date-group {
    margin-bottom: var(--space-sm);
  }

  .date-label {
    display: block;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding: var(--space-sm) var(--space-sm) 4px;
  }

  .session-item {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    width: 100%;
    padding: 8px var(--space-sm);
    border-radius: var(--radius-sm);
    text-align: left;
    transition: background var(--transition-fast);
    cursor: pointer;
  }

  .session-item:hover {
    background: rgba(122, 162, 247, 0.08);
  }

  .session-item.active {
    background: rgba(122, 162, 247, 0.15);
  }

  .session-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-dim);
    flex-shrink: 0;
  }

  .session-dot.dot-active {
    background: var(--success);
  }

  .session-dot.dot-done {
    background: var(--text-muted);
  }

  .session-info {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }

  .session-title {
    color: var(--text-secondary);
    font-size: 12px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .session-item:hover .session-title,
  .session-item.active .session-title {
    color: var(--text-primary);
  }

  .session-meta {
    color: var(--text-dim);
    font-size: 10px;
  }

  /* --- Bottom actions --- */
  .bottom-actions {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-sm) var(--space-md) var(--space-md);
    border-top: 1px solid var(--bg-surface);
    flex-shrink: 0;
  }

  .stat-line {
    color: var(--text-dim);
    font-size: 11px;
    padding-bottom: 4px;
  }

  .action-btn {
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 12px;
    text-align: left;
    transition: all var(--transition-fast);
  }

  .action-btn:hover {
    background: rgba(122, 162, 247, 0.1);
    color: var(--text-primary);
  }

  .update-info {
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    background: rgba(122, 162, 247, 0.08);
  }

  .update-text {
    font-size: 11px;
    color: var(--text-secondary);
    display: block;
    margin-bottom: 4px;
  }

  .update-btn {
    padding: 3px 8px;
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
