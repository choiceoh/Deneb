<script lang="ts">
  import { onMount } from "svelte";

  let { code, language = "" }: { code: string; language?: string } = $props();

  let container: HTMLDivElement;
  let editor: any = null;

  // Monaco is loaded lazily to avoid blocking initial render.
  onMount(async () => {
    const monaco = await import("monaco-editor");

    // Register Tokyo Night theme (once).
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

  let themeRegistered = false;
</script>

<div class="code-block">
  {#if language}
    <div class="code-lang">{language}</div>
  {/if}
  <div class="code-container" bind:this={container}></div>
</div>

<style>
  .code-block {
    border-radius: var(--radius-md);
    border: 1px solid var(--bg-surface);
    overflow: hidden;
    margin: var(--space-sm) 0;
  }

  .code-lang {
    padding: 6px 12px;
    background: rgba(26, 30, 48, 0.8);
    color: var(--text-muted);
    font-size: 11px;
    font-family: var(--font-mono);
    border-bottom: 1px solid var(--bg-surface);
  }

  .code-container {
    min-height: 48px;
    max-height: 400px;
  }
</style>
