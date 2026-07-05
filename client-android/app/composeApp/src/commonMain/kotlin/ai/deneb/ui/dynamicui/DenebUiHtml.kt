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
    private val voidTags = setOf("hr", "img", "input", "icon", "slider", "progress", "avatar", "point", "br")

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
    }

    // =====================================================================
    // Tokenizer + tree builder
    // =====================================================================

    private class Parser(private val src: String) {
        private var pos = 0
        private val stack = mutableListOf<OpenElem>()
        private val roots = mutableListOf<DenebUiNode>()

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
                attach(convert(el))
                return
            }
            stack.add(el)
        }

        private fun captureRawText(name: String, attrs: Map<String, String>) {
            val lower = src.lowercase()
            val end = lower.indexOf("</$name", pos)
            val raw: String
            if (end < 0) {
                raw = src.substring(pos)
                pos = src.length
            } else {
                raw = src.substring(pos, end)
                val gt = src.indexOf('>', end)
                pos = if (gt < 0) src.length else gt + 1
            }
            val el = OpenElem(name, attrs)
            el.text.add(decodeEntities(raw))
            attach(convert(el))
        }

        private fun handleClose(name: String) {
            if (name in voidTags) return
            val idx = stack.indexOfLast { it.tag == name }
            if (idx < 0) return // stray close: ignore
            while (stack.size > idx) closeTop()
        }

        private fun closeTop() {
            val el = stack.removeAt(stack.lastIndex)
            attach(convert(el))
        }

        private fun attach(v: Any?) {
            when (v) {
                null -> return

                is Structural -> stack.lastOrNull()?.structs?.add(v)

                // floating at root: drop
                is DenebUiNode ->
                    if (stack.isEmpty()) roots.add(v) else stack.last().children.add(v)
            }
        }

        private fun emitText(t: String) {
            if (t.isBlank()) return
            val decoded = decodeEntities(t).trim()
            val top = stack.lastOrNull()
            if (top == null) {
                roots.add(TextNode(value = decoded))
                return
            }
            top.text.add(decodeEntities(t))
            if (top.tag in containerTags) {
                top.children.add(TextNode(value = decoded))
            }
        }
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

            "row" -> RowNode(id = id, children = kids())

            "card" -> CardNode(id = id, children = kids())

            "box" -> BoxNode(id = id, children = kids(), contentAlignment = a["align"])

            "hr", "divider" -> DividerNode(id = id)

            "text" -> TextNode(
                id = id,
                value = inner,
                style = styleOf(a["style"]),
                bold = attrBool(a, "bold"),
                italic = attrBool(a, "italic"),
                color = a["color"],
            )

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

            "badge" -> BadgeNode(id = id, value = a["value"]?.takeIf { it.isNotEmpty() } ?: inner, color = a["color"])

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

            "progress" -> ProgressNode(id = id, value = attrFloat(a, "value"), label = a["label"])

            "alert" -> AlertNode(
                id = id,
                message = a["message"]?.takeIf { it.isNotEmpty() } ?: inner,
                title = a["title"],
                severity = severityOf(a["severity"]),
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
                    chartType = a["type"]?.takeIf { it.isNotEmpty() } ?: "bar",
                    labels = points.map { it.label }.toImmutableList(),
                    values = points.map { it.value }.toImmutableList(),
                    label = a["label"],
                )
            }

            "point" -> Structural.Point(
                label = a["label"] ?: "",
                value = a["value"]?.trim()?.toFloatOrNull() ?: 0f,
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

    // =====================================================================
    // Attribute coercion (lenient: bad values fall back to null/defaults)
    // =====================================================================

    private fun attrBool(a: Map<String, String>, key: String): Boolean? = a[key]?.let { truthy(it) }

    private fun attrInt(a: Map<String, String>, key: String): Int? = a[key]?.trim()?.toFloatOrNull()?.toInt()

    private fun attrFloat(a: Map<String, String>, key: String): Float? = a[key]?.trim()?.toFloatOrNull()

    private fun truthy(v: String): Boolean = v.trim().lowercase() !in setOf("false", "0", "no", "off")

    private fun styleOf(v: String?): TextNodeStyle? = when (v?.lowercase()) {
        "headline" -> TextNodeStyle.HEADLINE
        "title" -> TextNodeStyle.TITLE
        "body" -> TextNodeStyle.BODY
        "caption" -> TextNodeStyle.CAPTION
        else -> null
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
