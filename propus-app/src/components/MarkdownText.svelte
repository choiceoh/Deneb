<script lang="ts">
  import { Marked } from "marked";

  let { content }: { content: string } = $props();

  const marked = new Marked({
    breaks: true,
    gfm: true,
  });

  // Render markdown to HTML. Inline code is styled via global CSS.
  // Code fences are already handled by parseSegments, so text segments
  // should only contain inline markdown (bold, italic, lists, links, etc.)
  let html = $derived(marked.parse(content, { async: false }) as string);
</script>

<div class="markdown-body">{@html html}</div>
