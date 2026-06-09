package ai.deneb.ui.markdown

import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList

/**
 * Resolve reference-style links after block scanning. [BlockScanner] collects the
 * definitions ("[1]: https://…") and blanks their lines; this pass rewrites usages —
 * full "[text][label]" and collapsed "[text][]" — inside [Text] nodes into real [Link]s.
 *
 * A usage spanning multiple inline nodes (emphasis inside the bracket text, a code span
 * in the label) stays literal: LLM output uses plain labels, and partial rewrites would
 * be worse than none. Unresolved labels also stay literal.
 */
internal fun resolveReferenceLinks(
    blocks: ImmutableList<BlockNode>,
    defs: Map<String, String>,
): ImmutableList<BlockNode> = blocks.map { resolveBlock(it, defs) }.toImmutableList()

private fun resolveBlock(block: BlockNode, defs: Map<String, String>): BlockNode = when (block) {
    is Heading -> Heading(block.level, resolveInlines(block.inlines, defs))
    is Paragraph -> Paragraph(resolveInlines(block.inlines, defs))
    is Blockquote -> Blockquote(block.children.map { resolveBlock(it, defs) }.toImmutableList())
    is BulletList -> BulletList(block.items.map { resolveItem(it, defs) }.toImmutableList(), block.tight)
    is OrderedList -> OrderedList(
        block.start,
        block.items.map { resolveItem(it, defs) }.toImmutableList(),
        block.tight,
    )
    is Table -> Table(
        block.headers.map { resolveInlines(it, defs) }.toImmutableList(),
        block.alignments,
        block.rows.map { row -> row.map { resolveInlines(it, defs) }.toImmutableList() }.toImmutableList(),
    )
    else -> block
}

private fun resolveItem(item: ListItem, defs: Map<String, String>): ListItem =
    ListItem(item.children.map { resolveBlock(it, defs) }.toImmutableList(), item.checked)

private fun resolveInlines(
    inlines: ImmutableList<InlineNode>,
    defs: Map<String, String>,
): ImmutableList<InlineNode> {
    var changed = false
    val out = mutableListOf<InlineNode>()
    for (n in inlines) {
        when (n) {
            is Text -> {
                val expanded = expandRefLinks(n.value, defs)
                if (expanded == null) {
                    out += n
                } else {
                    changed = true
                    out += expanded
                }
            }

            is Strong -> {
                val c = resolveInlines(n.children, defs)
                if (c !== n.children) {
                    changed = true
                    out += Strong(c)
                } else {
                    out += n
                }
            }

            is Emphasis -> {
                val c = resolveInlines(n.children, defs)
                if (c !== n.children) {
                    changed = true
                    out += Emphasis(c)
                } else {
                    out += n
                }
            }

            is Strike -> {
                val c = resolveInlines(n.children, defs)
                if (c !== n.children) {
                    changed = true
                    out += Strike(c)
                } else {
                    out += n
                }
            }

            else -> out += n
        }
    }
    return if (changed) out.toImmutableList() else inlines
}

private val REF_LINK_REGEX = Regex("""\[([^\[\]]+)\]\[([^\[\]]*)\]""")

// Expand "[text][label]" occurrences in one Text node. Returns null when nothing resolved
// so callers can keep the original node (and its identity) untouched.
private fun expandRefLinks(text: String, defs: Map<String, String>): List<InlineNode>? {
    if ('[' !in text) return null
    var result: MutableList<InlineNode>? = null
    var pos = 0
    for (m in REF_LINK_REGEX.findAll(text)) {
        val label = m.groupValues[2].ifEmpty { m.groupValues[1] }.trim().lowercase()
        val href = defs[label] ?: continue
        val r = result ?: mutableListOf<InlineNode>().also { result = it }
        if (m.range.first > pos) r += Text(text.substring(pos, m.range.first))
        r += Link(href, persistentListOf(Text(m.groupValues[1])))
        pos = m.range.last + 1
    }
    val r = result ?: return null
    if (pos < text.length) r += Text(text.substring(pos))
    return r
}
