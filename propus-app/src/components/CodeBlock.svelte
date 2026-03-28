<script lang="ts">
  import { onMount } from "svelte";

  let { code, language = "" }: { code: string; language?: string } = $props();

  let container: HTMLDivElement;
  let editor: any = null;
  let monacoLoaded = $state(false);
  let copied = $state(false);

  async function copyToClipboard() {
    try {
      await navigator.clipboard.writeText(code);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = code;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      document.body.removeChild(ta);
    }
    copied = true;
    setTimeout(() => {
      copied = false;
    }, 2000);
  }

  // Module-level Monaco cache: loaded once, reused across all CodeBlock instances.
  let monacoModule: typeof import("monaco-editor") | null = null;
  let monacoLoading: Promise<typeof import("monaco-editor")> | null = null;
  let themeRegistered = false;

  async function loadMonaco() {
    if (monacoModule) return monacoModule;
    if (!monacoLoading) {
      monacoLoading = import("monaco-editor");
    }
    monacoModule = await monacoLoading;
    return monacoModule;
  }

  // Map common fence labels to Monaco language IDs.
  const langMap: Record<string, string> = {
    js: "javascript",
    ts: "typescript",
    py: "python",
    rb: "ruby",
    rs: "rust",
    sh: "shell",
    bash: "shell",
    zsh: "shell",
    yml: "yaml",
    dockerfile: "dockerfile",
    md: "markdown",
  };

  onMount(async () => {
    const monaco = await loadMonaco();
    monacoLoaded = true;

    // Register Tokyo Night theme once.
    if (!themeRegistered) {
      monaco.editor.defineTheme("tokyo-night", {
        base: "vs-dark",
        inherit: true,
        rules: [
          { token: "comment", foreground: "565f89", fontStyle: "italic" },
          { token: "keyword", foreground: "bb9af7" },
          { token: "string", foreground: "9ece6a" },
          { token: "number", foreground: "ff9e64" },
          { token: "type", foreground: "7dcfff" },
          { token: "function", foreground: "7aa2f7" },
          { token: "variable", foreground: "c0caf5" },
          { token: "operator", foreground: "89ddff" },
        ],
        colors: {
          "editor.background": "#141620",
          "editor.foreground": "#a9b1d6",
          "editor.lineHighlightBackground": "#1a1e30",
          "editorLineNumber.foreground": "#3b4261",
          "editorGutter.background": "#141620",
        },
      });
      themeRegistered = true;
    }

    const monacoLang = langMap[language] ?? language ?? "plaintext";

    editor = monaco.editor.create(container, {
      value: code,
      language: monacoLang,
      theme: "tokyo-night",
      readOnly: true,
      minimap: { enabled: false },
      lineNumbers: "on",
      scrollBeyondLastLine: false,
      wordWrap: "on",
      fontSize: 13,
      fontFamily: "var(--font-mono)",
      renderLineHighlight: "none",
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      scrollbar: {
        vertical: "hidden",
        horizontal: "hidden",
        handleMouseWheel: false,
      },
      domReadOnly: true,
      contextmenu: false,
      folding: false,
      glyphMargin: false,
      padding: { top: 12, bottom: 12 },
    });

    // Auto-size to content height.
    const lineCount = code.split("\n").length;
    const lineHeight = 20;
    const padding = 24;
    const height = Math.min(Math.max(lineCount * lineHeight + padding, 48), 400);
    container.style.height = `${height}px`;
    editor.layout();

    return () => editor?.dispose();
  });
</script>

<div class="code-block">
  <div class="code-header">
    <span class="code-lang">{language || ""}</span>
    <button class="copy-btn" onclick={copyToClipboard} title="복사">
      {copied ? "복사됨" : "복사"}
    </button>
  </div>
  {#if monacoLoaded}
    <div class="code-container" bind:this={container}></div>
  {:else}
    <pre class="code-fallback"><code>{code}</code></pre>
    <div class="code-container" bind:this={container} style="display:none"></div>
  {/if}
</div>

<style>
  .code-block {
    border-radius: var(--radius-md);
    border: 1px solid var(--bg-surface);
    overflow: hidden;
    margin: var(--space-sm) 0;
  }

  .code-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    background: rgba(26, 30, 48, 0.8);
    border-bottom: 1px solid var(--bg-surface);
  }

  .code-lang {
    color: var(--text-muted);
    font-size: 11px;
    font-family: var(--font-mono);
  }

  .copy-btn {
    background: transparent;
    border: 1px solid var(--bg-surface);
    border-radius: 4px;
    color: var(--text-muted);
    font-size: 11px;
    font-family: var(--font-mono);
    padding: 2px 8px;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }

  .copy-btn:hover {
    color: var(--accent-primary, #7aa2f7);
    border-color: var(--accent-primary, #7aa2f7);
  }

  .code-container {
    min-height: 48px;
    max-height: 400px;
  }

  .code-fallback {
    background: #141620;
    color: #a9b1d6;
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.5;
    padding: 12px;
    margin: 0;
    overflow-x: auto;
    max-height: 400px;
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
