package ai.deneb.ui.dynamicui

import kotlinx.collections.immutable.toImmutableList
import kotlinx.serialization.json.JsonPrimitive

/**
 * Labeled-HTML wire format for deneb-ui blocks (v2, 2026-07): parses the body of
 * a `deneb-ui` fence whose first character is `<` into a [DenebUiNode] tree.
 *
 * Rationale ("AST as HTML"): models carry deep pre-trained fluency in HTML, so
 * the custom-JSON repair pipeline this replaced is unnecessary — and HTML
 * degrades gracefully under stream truncation (EOF auto-close) where truncated
 * JSON was fatal.
 *
 * This is a deliberately small XML-lite tokenizer, NOT an HTML5 parser: no
 * foster-parenting, so real `<table>/<tr>/<td>` and custom tags coexist
 * predictably. Grammar single source of truth: docs/research/deneb-ui-html.md;
 * the gateway (denebui/html.go) and Andromeda (denebUiHtml.ts) port the same
 * rules — keep the shared test vectors in sync.
 */
object DenebUiHtml {

    /** Void tags never take children; a stray close tag is tolerated. */
    private val voidTags = setOf("hr", "img", "input", "icon", "slider", "progress", "avatar", "point", "br", "spacer")

    /** Raw-text tags capture verbatim content up to their literal close tag. */
    private val rawTextTags = setOf("markdown", "code")

    /** Opening tag → open sibling tags it implicitly closes (HTML5 habits). */
    private val autoClose = mapOf(
        "li" to setOf("li"),
        "option" to setOf("option"),
        "td" to setOf("td", "th"),
        "th" to setOf("td", "th"),
        "tr" to setOf("td", "th", "tr"),
        "tab" to setOf("tab"),
        "chip" to setOf("chip"),
        "point" to setOf("point"),
    )

    /** Containers whose bare text runs become implicit text nodes. */
    private val containerTags = setOf("column", "col", "row", "card", "box", "accordion", "li", "tab")

    /**
     * Inline HTML formatting habits with no node of their own: they merge back
     * into the parent text flow as markdown-marked runs ("**"/"*") the inline
     * tokenizer already draws — content survives instead of the subtree
     * dropping. Empty marker = keep bare text.
     */
    private val inlineTags = mapOf(
        "b" to "**", "strong" to "**", "i" to "*", "em" to "*",
        "u" to "", "s" to "", "del" to "", "strike" to "", "mark" to "",
        "small" to "", "span" to "", "sub" to "", "sup" to "", "a" to "",
    )

    /**
     * Structural HTML wrappers (div soup) models emit out of pre-trained
     * habit. They produce no node: children hoist to the parent and bare text
     * becomes implicit text nodes.
     */
    private val genericTags = setOf(
        "div", "section", "article", "header", "footer", "main", "aside", "figure", "center", "nav",
        "thead", "tbody", "tfoot",
    )

    /**
     * Every tag [convert] maps to a node or structural. Tags in none of the
     * tables unwrap like [genericTags] (the gateway validator reports them),
     * so content survives typos.
     */
    private val knownTags = setOf(
        "column", "col", "row", "card", "box", "hr", "divider", "text", "markdown",
        "img", "image", "icon", "code", "blockquote", "quote", "badge", "stat",
        "avatar", "progress", "alert", "countdown", "chart", "point", "table",
        "tr", "td", "th", "ul", "ol", "list", "li", "tabs", "tab", "accordion",
        "button", "input", "textarea", "checkbox", "switch", "select",
        "radio-group", "radiogroup", "option", "slider", "chips", "chip-group",
        "chip", "br", "p", "h1", "h2", "h3", "h4", "h5", "h6",
        "title", "label", "spacer", "kv",
    )

    /**
     * Whether bare text inside a tag surfaces as implicit child nodes
     * (containers, generic wrappers, unknown tags) rather than feeding the
     * element's own value slot (text/badge/li/… and inline tags).
     */
    private fun treatsTextAsChildren(tag: String): Boolean = when {
        tag in containerTags || tag in genericTags -> true
        tag in inlineTags -> false
        else -> tag !in knownTags
    }

    /** Parse a fence body into a node, or null when nothing usable parsed. */
    fun parse(body: String): DenebUiNode? {
        val roots = Parser(body.trim()).parseNodes()
        return when {
            roots.isEmpty() -> null
            roots.size == 1 -> roots[0]
            else -> ColumnNode(children = roots.toImmutableList())
        }
    }

    // =====================================================================
    // Structural intermediates (consumed by their parent element)
    // =====================================================================

    private sealed interface Structural {
        data class Option(val text: String, val selected: Boolean) : Structural
        data class Chip(val label: String, val value: String) : Structural
        data class Tab(val label: String, val children: List<DenebUiNode>) : Structural
        data class Row(val cells: List<Cell>) : Structural
        data class Cell(val text: String, val header: Boolean) : Structural
        data class Point(val label: String, val value: Float) : Structural
        data class Item(val text: String, val children: List<DenebUiNode>) : Structural
    }

    private class OpenElem(val tag: String, val attrs: Map<String, String>) {
        val children = mutableListOf<DenebUiNode>()
        val structs = mutableListOf<Structural>()
        val text = mutableListOf<String>()

        /** Buffered implicit-text runs, flushed as one merged node. */
        val pending = mutableListOf<String>()

        /**
         * A whitespace-only run arrived after existing text; the next run keeps
         * one separating space so inline merges don't glue ("**A** **B**").
         */
        var pendingSpace = false
    }

    // =====================================================================
    // Tokenizer + tree builder
    // =====================================================================

    private class Parser(private val src: String) {
        private var pos = 0
        private val stack = mutableListOf<OpenElem>()
        private val roots = mutableListOf<DenebUiNode>()
        private val rootPending = mutableListOf<String>()
        private var rootPendingSpace = false

        fun parseNodes(): List<DenebUiNode> {
            while (pos < src.length) {
                val lt = src.indexOf('<', pos)
                if (lt < 0) {
                    emitText(src.substring(pos))
                    break
                }
                if (lt > pos) {
                    emitText(src.substring(pos, lt))
                    pos = lt
                }
                if (!parseTag()) {
                    emitText("<")
                    pos++
                }
            }
            while (stack.isNotEmpty()) closeTop()
            flushRootPending()
            return roots
        }

        private fun parseTag(): Boolean {
            val i = pos
            if (i + 1 >= src.length) return false
            when {
                src.startsWith("<!--", i) -> {
                    val end = src.indexOf("-->", i + 4)
                    pos = if (end < 0) src.length else end + 3
                    return true
                }

                src[i + 1] == '!' -> {
                    val end = src.indexOf('>', i)
                    pos = if (end < 0) src.length else end + 1
                    return true
                }

                src[i + 1] == '/' -> {
                    val end = src.indexOf('>', i)
                    if (end < 0) return false
                    val name = src.substring(i + 2, end).trim().lowercase()
                    pos = end + 1
                    handleClose(name)
                    return true
                }
            }
            if (!src[i + 1].isNameStart()) return false
            var j = i + 1
            while (j < src.length && src[j].isNameChar()) j++
            val name = src.substring(i + 1, j).lowercase()
            val (attrs, end, selfClose) = parseAttrs(j)
            pos = end
            handleOpen(name, attrs, selfClose)
            return true
        }

        private fun parseAttrs(start: Int): Triple<Map<String, String>, Int, Boolean> {
            val attrs = mutableMapOf<String, String>()
            var j = start
            var selfClose = false
            while (true) {
                while (j < src.length && src[j].isWhitespace()) j++
                if (j >= src.length) return Triple(attrs, j, selfClose) // truncated tag → swallow
                when (src[j]) {
                    '>' -> return Triple(attrs, j + 1, selfClose)

                    '/' -> {
                        selfClose = true
                        j++
                        continue
                    }
                }
                if (!src[j].isNameStart()) {
                    j++
                    continue
                }
                var k = j
                while (k < src.length && src[k].isNameChar()) k++
                val key = src.substring(j, k).lowercase()
                j = k
                while (j < src.length && src[j].isWhitespace()) j++
                var value = "true" // boolean attribute default
                if (j < src.length && src[j] == '=') {
                    j++
                    while (j < src.length && src[j].isWhitespace()) j++
                    if (j < src.length && (src[j] == '"' || src[j] == '\'')) {
                        val q = src[j]
                        j++
                        k = j
                        while (k < src.length && src[k] != q) k++
                        value = src.substring(j, k)
                        j = minOf(k + 1, src.length)
                    } else {
                        k = j
                        while (k < src.length && !src[k].isWhitespace() && src[k] != '>' && src[k] != '/') k++
                        value = src.substring(j, k)
                        j = k
                    }
                }
                attrs[key] = decodeEntities(value)
            }
        }

        private fun handleOpen(name: String, attrs: Map<String, String>, selfClose: Boolean) {
            autoClose[name]?.let { closers ->
                while (stack.isNotEmpty() && stack.last().tag in closers) closeTop()
            }
            if (name in rawTextTags && !selfClose) {
                captureRawText(name, attrs)
                return
            }
            val el = OpenElem(name, attrs)
            if (name in voidTags || selfClose) {
                // Self-closed inline/generic/unknown tags carry no content.
                if (name in inlineTags || name in genericTags || name !in knownTags) return
                attach(convert(el))
                return
            }
            stack.add(el)
        }

        private fun captureRawText(name: String, attrs: Map<String, String>) {
            val end = indexOfCloseTag(src, pos, name)
            val raw: String
            if (end < 0) {
                raw = src.substring(pos)
                pos = src.length
            } else {
                raw = src.substring(pos, end)
                val gt = src.indexOf('>', end)
                pos = if (gt < 0) src.length else gt + 1
            }
            val decoded = decodeEntities(raw)
            // Inline habit: <code> inside a text flow merges as a backtick run
            // instead of breaking the sentence into a block node.
            if (name == "code" && inlineCodeContext()) {
                decoded.trim().takeIf { it.isNotEmpty() }?.let { emitRun("`$it`") }
                return
            }
            val el = OpenElem(name, attrs)
            el.text.add(decoded)
            attach(convert(el))
        }

        /** Raw `<code>` merges into text flow when the parent is a text node or inline tag. */
        private fun inlineCodeContext(): Boolean {
            val tag = stack.lastOrNull()?.tag ?: return false
            return tag == "text" || tag in inlineTags
        }

        /**
         * Absolute index of the first `</name` at or after [from], comparing the
         * (ASCII, whitelist-only) tag name case-insensitively via manual folding.
         * Never lowercase the whole source for index math: Unicode case mapping
         * can change string length (e.g. 'İ' → "i̇"), skewing indexes into the
         * original — the fuzzer-found crash in the Go port of this parser.
         */
        private fun indexOfCloseTag(s: String, from: Int, name: String): Int {
            val n = name.length
            var i = from
            while (i + 2 + n <= s.length) {
                if (s[i] == '<' && s[i + 1] == '/') {
                    var match = true
                    for (k in 0 until n) {
                        var c = s[i + 2 + k]
                        if (c in 'A'..'Z') c += 32
                        if (c != name[k]) {
                            match = false
                            break
                        }
                    }
                    if (match) return i
                }
                i++
            }
            return -1
        }

        private fun handleClose(name: String) {
            if (name in voidTags) return
            val idx = stack.indexOfLast { it.tag == name }
            if (idx < 0) return // stray close: ignore
            while (stack.size > idx) closeTop()
        }

        private fun closeTop() {
            val el = stack.removeAt(stack.lastIndex)
            inlineTags[el.tag]?.let { marker ->
                emitInline(el, marker)
                return
            }
            if (el.tag in genericTags || el.tag !in knownTags) {
                // Unwrap: the wrapper produces no node; its children (incl.
                // flushed implicit text) hoist to the parent in source order.
                // Structural intermediates hoist too, so <thead>/<tbody> table
                // rows reach the enclosing <table> instead of vanishing.
                flushPending(el)
                el.children.forEach { attach(it) }
                el.structs.forEach { attach(it) }
                return
            }
            flushPending(el)
            attach(convert(el))
        }

        /**
         * Merges an inline formatting element back into the parent text flow
         * (`<b>중요</b>` → `**중요**`). Plain-value slots (badge, button
         * labels) receive bare text — literal markers would render as noise
         * there. Real child nodes (rare) hoist to the parent afterwards.
         */
        private fun emitInline(el: OpenElem, marker: String) {
            val inner = el.text.joinToString("").trim()
            if (inner.isNotEmpty()) {
                val run = when {
                    !inlineMarkupAllowed() -> inner
                    el.tag == "a" && !el.attrs["href"].isNullOrEmpty() -> "[$inner](${el.attrs["href"]})"
                    el.tag == "a" || marker.isEmpty() -> inner
                    else -> "$marker$inner$marker"
                }
                emitRun(run)
            }
            el.children.forEach { attach(it) }
        }

        /** Markdown markers only where inline markdown renders (text/containers/root). */
        private fun inlineMarkupAllowed(): Boolean {
            val tag = stack.lastOrNull()?.tag ?: return true
            return tag == "text" || treatsTextAsChildren(tag)
        }

        private fun attach(v: Any?) {
            if (v != null) {
                // Whitespace before a child is layout, not a run separator.
                stack.lastOrNull()?.pendingSpace = false
            }
            when (v) {
                null -> return

                is Structural -> stack.lastOrNull()?.structs?.add(v)

                // floating at root: drop
                is DenebUiNode -> {
                    val top = stack.lastOrNull()
                    if (top == null) {
                        flushRootPending()
                        rootPendingSpace = false
                        roots.add(v)
                    } else {
                        flushPending(top)
                        top.children.add(v)
                    }
                }
            }
        }

        private fun emitText(t: String) {
            if (t.isBlank()) {
                markPendingSpace()
                return
            }
            emitRun(decodeEntities(t))
        }

        /**
         * Remembers that a whitespace-only run arrived after existing text, so
         * the next run in the same flow keeps a single separating space —
         * dropping it glues inline markers ("**A****B**").
         */
        private fun markPendingSpace() {
            val top = stack.lastOrNull()
            if (top == null) {
                if (rootPending.isNotEmpty()) rootPendingSpace = true
                return
            }
            if (top.text.isNotEmpty()) top.pendingSpace = true
        }

        /** Adds an already-decoded text run (entity decoding must not repeat on inline re-emits). */
        private fun emitRun(t: String) {
            if (t.isBlank()) return
            val top = stack.lastOrNull()
            if (top == null) {
                val run = if (rootPendingSpace) " $t" else t
                rootPendingSpace = false
                rootPending.add(run)
                return
            }
            val run = if (top.pendingSpace) " $t" else t
            top.pendingSpace = false
            top.text.add(run)
            if (treatsTextAsChildren(top.tag)) top.pending.add(run)
        }

        /**
         * Materializes buffered text runs as one merged implicit node. Merging
         * keeps sentences split by inline tags whole and lets markdown block
         * structure be recognized.
         */
        private fun flushPending(el: OpenElem) {
            if (el.pending.isEmpty()) return
            val node = textBlockNode(el.pending.joinToString(""))
            el.pending.clear()
            el.children.add(node)
        }

        private fun flushRootPending() {
            if (rootPending.isEmpty()) return
            val node = textBlockNode(rootPending.joinToString(""))
            rootPending.clear()
            roots.add(node)
        }
    }

    /**
     * Wraps a merged text run as an implicit node — as markdown when the run
     * carries markdown block structure (auto-correcting the "markdown table
     * inside a card" habit: markdown nodes route through the full markdown
     * renderer, tables included), else as plain text.
     */
    private fun textBlockNode(s: String): DenebUiNode {
        val t = s.trim()
        return if (looksLikeMarkdownBlock(t)) MarkdownNode(value = t) else TextNode(value = t)
    }

    /**
     * Whether text carries markdown block structure (table rows, headings,
     * list runs, fences) that a plain text node would render broken.
     * Conservative: single bullets or lone pipes stay text.
     */
    private fun looksLikeMarkdownBlock(s: String): Boolean {
        if ("```" in s) return true
        var pipeRows = 0
        var bullets = 0
        for (line in s.lineSequence()) {
            val t = line.trim()
            if (t.isEmpty()) continue
            if (t.startsWith("|")) {
                if (++pipeRows >= 2) return true
                continue
            }
            if (isMarkdownHeading(t)) return true
            if (isMarkdownBullet(t) && ++bullets >= 2) return true
        }
        return false
    }

    private fun isMarkdownHeading(t: String): Boolean {
        var n = 0
        while (n < t.length && t[n] == '#') n++
        return n in 1..6 && n < t.length && t[n] == ' '
    }

    private fun isMarkdownBullet(t: String): Boolean {
        if (t.length >= 2 && (t[0] == '-' || t[0] == '*') && t[1] == ' ') return true
        if (t.startsWith("• ")) return true
        var i = 0
        while (i < t.length && t[i] in '0'..'9') i++
        return i >= 1 && i + 1 < t.length && (t[i] == '.' || t[i] == ')') && t[i + 1] == ' '
    }

    // =====================================================================
    // Element → node conversion
    // =====================================================================

    private fun convert(el: OpenElem): Any? {
        val a = el.attrs
        val inner = el.text.joinToString("").trim()
        val id = a["id"]?.takeIf { it.isNotEmpty() }
        val kids = { el.children.toImmutableList() }
        return when (el.tag) {
            "column", "col" -> ColumnNode(id = id, children = kids())

            "row" -> RowNode(id = id, children = kids(), longPressAction = longPressFromAttrs(a))

            "card" -> CardNode(id = id, children = kids())

            "box" -> BoxNode(id = id, children = kids(), contentAlignment = a["align"])

            // spacer: invented-but-frequent alias — breathing room ≈ divider.
            "hr", "divider", "spacer" -> DividerNode(id = id)

            "text" -> if (looksLikeMarkdownBlock(inner)) {
                // Whole markdown blocks stuffed into <text> upgrade to a
                // markdown node so they render structured.
                MarkdownNode(id = id, value = inner)
            } else {
                TextNode(
                    id = id,
                    value = inner,
                    style = styleOf(a["style"]),
                    bold = attrBool(a, "bold"),
                    italic = attrBool(a, "italic"),
                    color = a["color"],
                    longPressAction = longPressFromAttrs(a),
                )
            }

            // HTML fluency aliases: paragraphs and headings map onto text nodes.
            // title/label/kv are invented-but-frequent model habits promoted to
            // proper typography (2026-07-18 reject telemetry; gateway parity).
            "p", "h1", "h2", "h3", "h4", "h5", "h6", "title", "label", "kv" -> {
                var value = inner
                if (el.tag == "kv") {
                    val k = a["label"].orEmpty()
                    if (k.isNotEmpty() && value.isNotEmpty()) value = "$k — $value"
                }
                if (value.isEmpty() && el.children.isEmpty()) return null
                val textNode = TextNode(
                    id = id,
                    value = value,
                    style = when (el.tag) {
                        "h1" -> TextNodeStyle.HEADLINE
                        "h2", "h3", "title" -> TextNodeStyle.TITLE
                        "label" -> TextNodeStyle.CAPTION
                        else -> null
                    },
                    bold = if (el.tag in setOf("h4", "h5", "h6")) true else null,
                )
                if (el.children.isEmpty()) {
                    textNode
                } else {
                    // Block children inside a paragraph: keep both, text first.
                    val kids = buildList {
                        if (value.isNotEmpty()) add(textNode)
                        addAll(el.children)
                    }
                    ColumnNode(children = kids.toImmutableList())
                }
            }

            "markdown" -> MarkdownNode(id = id, value = inner)

            "img", "image" -> ImageNode(
                id = id,
                url = a["src"] ?: a["url"] ?: "",
                alt = a["alt"],
                height = attrInt(a, "height"),
                aspectRatio = attrFloat(a, "aspect-ratio"),
            )

            "icon" -> IconNode(id = id, name = a["name"] ?: "", size = attrInt(a, "size"), color = a["color"])

            "code" -> CodeNode(id = id, code = inner, language = a["language"] ?: a["lang"])

            "blockquote", "quote" -> QuoteNode(id = id, text = inner, source = a["source"])

            "badge" -> BadgeNode(
                id = id,
                value = a["value"]?.takeIf { it.isNotEmpty() } ?: inner,
                color = canonBadgeColor(a["color"]),
            )

            "stat" -> StatNode(
                id = id,
                value = a["value"]?.takeIf { it.isNotEmpty() } ?: inner,
                label = a["label"] ?: "",
                description = a["description"],
            )

            "avatar" -> AvatarNode(
                id = id,
                name = a["name"],
                imageUrl = a["src"] ?: a["image-url"],
                size = attrInt(a, "size"),
            )

            "progress" -> ProgressNode(
                id = id,
                // Percent tolerance: "68" / "68%" mean 68% — the 0..1 contract
                // only applies to values already in range.
                value = attrFloat(a, "value")?.let { if (it > 1f) it / 100f else it }?.coerceIn(0f, 1f),
                label = a["label"],
            )

            "alert" -> AlertNode(
                id = id,
                message = a["message"]?.takeIf { it.isNotEmpty() } ?: inner,
                title = a["title"],
                severity = severityOf(canonSeverity(a["severity"])),
            )

            "countdown" -> CountdownNode(
                id = id,
                seconds = attrInt(a, "seconds") ?: 0,
                label = a["label"]?.takeIf { it.isNotEmpty() } ?: inner.takeIf { it.isNotEmpty() },
                action = actionFromAttrs(a),
            )

            "chart" -> {
                val points = el.structs.filterIsInstance<Structural.Point>()
                ChartNode(
                    id = id,
                    chartType = canonChartType(a["type"]) ?: "bar",
                    labels = points.map { it.label }.toImmutableList(),
                    values = points.map { it.value }.toImmutableList(),
                    label = a["label"],
                )
            }

            "point" -> Structural.Point(
                label = a["label"] ?: "",
                value = lenientFloat(a["value"]) ?: 0f,
            )

            "table" -> {
                var headers = listOf<String>()
                val rows = mutableListOf<List<String>>()
                for (tr in el.structs.filterIsInstance<Structural.Row>()) {
                    val texts = tr.cells.map { it.text }
                    if (tr.cells.any { it.header } && headers.isEmpty()) headers = texts else rows.add(texts)
                }
                TableNode(
                    id = id,
                    headers = headers.toImmutableList(),
                    rows = rows.map { it.toImmutableList() }.toImmutableList(),
                )
            }

            "tr" -> Structural.Row(el.structs.filterIsInstance<Structural.Cell>())

            "td" -> Structural.Cell(inner, header = false)

            "th" -> Structural.Cell(inner, header = true)

            "ul", "ol", "list" -> {
                val items = el.structs.filterIsInstance<Structural.Item>().map { li ->
                    when (li.children.size) {
                        0 -> TextNode(value = li.text)
                        1 -> li.children[0]
                        else -> ColumnNode(children = li.children.toImmutableList())
                    }
                }
                ListNode(
                    id = id,
                    items = items.toImmutableList(),
                    ordered = if (el.tag == "ol" || a["ordered"]?.let { truthy(it) } == true) true else null,
                )
            }

            "li" -> Structural.Item(inner, el.children.toList())

            "tabs" -> TabsNode(
                id = id,
                tabs = el.structs.filterIsInstance<Structural.Tab>()
                    .map { TabItem(label = it.label, children = it.children.toImmutableList()) }
                    .toImmutableList(),
                selectedIndex = attrInt(a, "selected-index"),
            )

            "tab" -> Structural.Tab(a["label"] ?: "", el.children.toList())

            "accordion" -> AccordionNode(
                id = id,
                title = a["title"] ?: "",
                children = kids(),
                expanded = attrBool(a, "expanded"),
            )

            "button" -> ButtonNode(
                id = id,
                label = a["label"]?.takeIf { it.isNotEmpty() } ?: inner,
                action = actionFromAttrs(a),
                variant = variantOf(a["variant"]),
                enabled = attrBool(a, "enabled"),
            )

            "input" -> when (a["type"]?.lowercase()) {
                "date" -> DateInputNode(
                    id = a["id"] ?: "",
                    label = a["label"],
                    value = a["value"],
                    required = attrBool(a, "required"),
                )

                "time" -> TimeInputNode(
                    id = a["id"] ?: "",
                    label = a["label"],
                    value = a["value"],
                    required = attrBool(a, "required"),
                )

                "checkbox" -> CheckboxNode(
                    id = a["id"] ?: "",
                    label = a["label"] ?: "",
                    checked = attrBool(a, "checked"),
                )

                else -> textInput(a, multilineDefault = null, innerValue = null)
            }

            "textarea" -> textInput(a, multilineDefault = true, innerValue = inner.takeIf { it.isNotEmpty() })

            "checkbox" -> CheckboxNode(
                id = a["id"] ?: "",
                label = a["label"]?.takeIf { it.isNotEmpty() } ?: inner,
                checked = attrBool(a, "checked"),
            )

            "switch" -> SwitchNode(
                id = a["id"] ?: "",
                label = a["label"]?.takeIf { it.isNotEmpty() } ?: inner,
                checked = attrBool(a, "checked"),
            )

            "select" -> {
                val opts = el.structs.filterIsInstance<Structural.Option>()
                SelectNode(
                    id = a["id"] ?: "",
                    label = a["label"],
                    options = opts.map { it.text }.toImmutableList(),
                    selected = a["selected"]?.takeIf { it.isNotEmpty() }
                        ?: opts.lastOrNull { it.selected }?.text,
                    placeholder = a["placeholder"],
                    required = attrBool(a, "required"),
                )
            }

            "radio-group", "radiogroup" -> {
                val opts = el.structs.filterIsInstance<Structural.Option>()
                RadioGroupNode(
                    id = a["id"] ?: "",
                    label = a["label"],
                    options = opts.map { it.text }.toImmutableList(),
                    selected = a["selected"]?.takeIf { it.isNotEmpty() }
                        ?: opts.lastOrNull { it.selected }?.text,
                    required = attrBool(a, "required"),
                )
            }

            "option" -> Structural.Option(inner, selected = a["selected"]?.let { truthy(it) } == true)

            "slider" -> SliderNode(
                id = a["id"] ?: "",
                label = a["label"],
                value = attrFloat(a, "value"),
                min = attrFloat(a, "min"),
                max = attrFloat(a, "max"),
                step = attrFloat(a, "step"),
            )

            "chips", "chip-group" -> ChipGroupNode(
                id = a["id"] ?: "",
                chips = el.structs.filterIsInstance<Structural.Chip>()
                    .map { ChipItem(label = it.label, value = it.value) }
                    .toImmutableList(),
                selection = a["selection"]?.takeIf { it.isNotEmpty() } ?: "single",
                required = attrBool(a, "required"),
            )

            "chip" -> Structural.Chip(label = inner, value = a["value"]?.takeIf { it.isNotEmpty() } ?: inner)

            "br" -> null

            else -> null // unknown tag: skip subtree (server validation reports it)
        }
    }

    private fun textInput(a: Map<String, String>, multilineDefault: Boolean?, innerValue: String?): TextInputNode = TextInputNode(
        id = a["id"] ?: "",
        label = a["label"],
        placeholder = a["placeholder"],
        value = a["value"]?.takeIf { it.isNotEmpty() } ?: innerValue,
        multiline = attrBool(a, "multiline") ?: multilineDefault,
        keyboard = a["keyboard"],
        required = attrBool(a, "required"),
    )

    /** Action attributes on button/countdown. Precedence: event > href > toggle > copy. */
    private fun actionFromAttrs(a: Map<String, String>): UiAction? {
        a["event"]?.takeIf { it.isNotEmpty() }?.let { event ->
            val data = a.entries
                .filter { it.key.startsWith("data-") && it.key.length > 5 }
                .associate { it.key.removePrefix("data-") to JsonPrimitive(it.value) }
            val collect = a["collect"]?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }
            return CallbackAction(
                event = event,
                data = data.takeIf { it.isNotEmpty() },
                collectFrom = collect?.takeIf { it.isNotEmpty() },
            )
        }
        a["href"]?.takeIf { it.isNotEmpty() }?.let { return OpenUrlAction(url = it) }
        a["toggle"]?.takeIf { it.isNotEmpty() }?.let { return ToggleAction(targetId = it) }
        a["copy"]?.takeIf { it.isNotEmpty() }?.let { return CopyToClipboardAction(text = it) }
        return null
    }

    /** longpress="event" (+ data-*) → a press-hold callback, distinct from the
     *  tap action's event=. Gateway parity (html_convert.longPressActionFromAttrs). */
    private fun longPressFromAttrs(a: Map<String, String>): UiAction? {
        val event = a["longpress"]?.trim()?.takeIf { it.isNotEmpty() } ?: return null
        val data = a.entries
            .filter { it.key.startsWith("data-") && it.key.length > 5 }
            .associate { it.key.removePrefix("data-") to JsonPrimitive(it.value) }
        return CallbackAction(event = event, data = data.takeIf { it.isNotEmpty() })
    }

    // =====================================================================
    // Attribute coercion (lenient: bad values fall back to null/defaults)
    // =====================================================================

    private fun attrBool(a: Map<String, String>, key: String): Boolean? = a[key]?.let { truthy(it) }

    private fun attrInt(a: Map<String, String>, key: String): Int? = lenientFloat(a[key])?.toInt()

    private fun attrFloat(a: Map<String, String>, key: String): Float? = lenientFloat(a[key])

    /**
     * Extracts a number from a lenient attribute value: exact floats parse
     * as-is; otherwise units, thousands commas, and stray symbols are
     * tolerated ("1,200톤" → 1200, "68%" → 68, "16px" → 16).
     */
    private fun lenientFloat(v: String?): Float? {
        val t = v?.trim().orEmpty()
        if (t.isEmpty()) return null
        t.toFloatOrNull()?.let { return it }
        var start = -1
        for (i in t.indices) {
            if (t[i] in '0'..'9') {
                start = i
                break
            }
        }
        if (start < 0) return null
        val b = StringBuilder()
        if (start > 0 && t[start - 1] == '-') b.append('-')
        var dot = false
        for (i in start until t.length) {
            val c = t[i]
            when {
                c in '0'..'9' -> b.append(c)

                c == ',' -> Unit

                // thousands separator: skip
                c == '.' && !dot -> {
                    dot = true
                    b.append(c)
                }

                else -> return b.toString().trimEnd('.').toFloatOrNull()
            }
        }
        return b.toString().trimEnd('.').toFloatOrNull()
    }

    private fun truthy(v: String): Boolean = v.trim().lowercase() !in setOf("false", "0", "no", "off")

    private fun styleOf(v: String?): TextNodeStyle? = when (v?.lowercase()) {
        "headline", "heading", "header" -> TextNodeStyle.HEADLINE
        "title", "subtitle", "subheading" -> TextNodeStyle.TITLE
        "body" -> TextNodeStyle.BODY
        "caption" -> TextNodeStyle.CAPTION
        else -> null
    }

    /** Folds common CSS color words onto the badge tint enum. */
    private fun canonBadgeColor(v: String?): String? = when (v?.trim()?.lowercase()) {
        "red" -> "error"
        "green" -> "success"
        "yellow", "amber", "orange" -> "warning"
        "blue" -> "primary"
        "gray", "grey", "neutral" -> "secondary"
        else -> v
    }

    /** Folds severity synonyms onto the alert enum. */
    private fun canonSeverity(v: String?): String? = when (v?.trim()?.lowercase()) {
        "warn", "caution" -> "warning"
        "danger", "critical", "fatal" -> "error"
        "ok", "done" -> "success"
        "note", "notice", "information" -> "info"
        else -> v
    }

    /** Folds chart-type synonyms onto bar/line. */
    private fun canonChartType(v: String?): String? = when (v?.trim()?.lowercase()) {
        "bars", "column", "columns" -> "bar"
        "lines", "area", "trend" -> "line"
        else -> v?.takeIf { it.isNotEmpty() }
    }

    private fun variantOf(v: String?): ButtonVariant? = when (v?.lowercase()) {
        "filled" -> ButtonVariant.FILLED
        "outlined" -> ButtonVariant.OUTLINED
        "text" -> ButtonVariant.TEXT
        "tonal" -> ButtonVariant.TONAL
        else -> null
    }

    private fun severityOf(v: String?): AlertSeverity? = when (v?.lowercase()) {
        "info" -> AlertSeverity.INFO
        "success" -> AlertSeverity.SUCCESS
        "warning" -> AlertSeverity.WARNING
        "error" -> AlertSeverity.ERROR
        else -> null
    }

    // =====================================================================
    // Entities
    // =====================================================================

    private fun Char.isNameStart(): Boolean = this in 'a'..'z' || this in 'A'..'Z'

    private fun Char.isNameChar(): Boolean = isNameStart() || this in '0'..'9' || this == '-' || this == '_'

    /** Decodes the grammar's small entity set: named basics + numeric refs. */
    internal fun decodeEntities(s: String): String {
        if ('&' !in s) return s
        val b = StringBuilder(s.length)
        var i = 0
        while (i < s.length) {
            val c = s[i]
            if (c != '&') {
                b.append(c)
                i++
                continue
            }
            val semi = s.indexOf(';', i)
            if (semi < 0 || semi - i > 10) {
                b.append(c)
                i++
                continue
            }
            val ent = s.substring(i + 1, semi)
            val decoded: String? = when (ent.lowercase()) {
                "lt" -> "<"

                "gt" -> ">"

                "amp" -> "&"

                "quot" -> "\""

                "apos" -> "'"

                "nbsp" -> " "

                else -> if (ent.startsWith("#")) {
                    val code = if (ent.length > 1 && (ent[1] == 'x' || ent[1] == 'X')) {
                        ent.drop(2).toIntOrNull(16)
                    } else {
                        ent.drop(1).toIntOrNull()
                    }
                    code?.takeIf { it > 0 }?.let { runCatching { it.toChar().toString() }.getOrNull() }
                } else {
                    null
                }
            }
            if (decoded == null) {
                b.append(c)
                i++
            } else {
                b.append(decoded)
                i = semi + 1
            }
        }
        return b.toString()
    }
}
