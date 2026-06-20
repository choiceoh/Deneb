@file:OptIn(androidx.compose.ui.test.ExperimentalTestApi::class)
@file:Suppress("DEPRECATION") // runDesktopComposeUiTest v1: auto-advancing clock, simplest for a one-shot harness

package ai.deneb

import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.LightColorScheme
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsNode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.ComposeUiTest
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.runDesktopComposeUiTest
import androidx.compose.ui.unit.dp

/**
 * Headless semantics inspector + driver — a vision-free way for an AI agent to SEE and
 * DRIVE a screen as TEXT. Composes a registered screen body under the Compose UI test
 * harness (Mobile.Android, phone size), dumps its semantics tree (every node's text,
 * role, clickable flag, bounds — the same tree accessibility uses, so Korean is exact,
 * unlike OCR), then applies a sequence of actions (click by node text), re-dumping after
 * each so a state change is visible. Wired by scripts/dev/ui-inspect.sh + the
 * previewInspect Gradle task. Siblings: RenderPreview.kt (PNG) and native-app.sh (live,
 * pixel/OCR). Driven by system properties: deneb.screen, deneb.actions, deneb.dark.
 */
// Inspector-only demo screens layered on top of the shared previewScreens registry
// (RenderPreview.kt): a synthetic settings group, and a tiny STATEFUL counter whose
// state change is visible across a re-dump. Real app screens come from previewScreens,
// so the same registry feeds both the PNG renderer and this inspector.
private val localScreens: Map<String, @Composable (ColorScheme) -> Unit> = mapOf(
    "settings" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebGroup(label = "환경설정") {
                DenebListRow(title = "게이트웨이", subtitle = "연결됨", onClick = {})
                DenebListRow(title = "모델", subtitle = "dsv4 · wormhole", onClick = {})
                DenebListRow(title = "알림", onClick = {}, divider = false)
            }
        }
    },
    "counter" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            var n by remember { mutableStateOf(0) }
            Column(Modifier.padding(16.dp)) {
                Text("카운터: $n")
                Button(onClick = { n++ }) { Text("증가") }
            }
        }
    },
)

/** Every inspectable screen: the shared registry (real app screens) + local demos. */
private val screens: Map<String, @Composable (ColorScheme) -> Unit> = previewScreens + localScreens

fun main() {
    val name = System.getProperty("deneb.screen", "").trim()
    val dark = System.getProperty("deneb.dark", "false").toBoolean()
    val actions = System.getProperty("deneb.actions", "")
        .split(";").map { it.trim() }.filter { it.isNotEmpty() }

    println("=== UI-INSPECT screen=$name theme=${if (dark) "dark" else "light"} ===")
    val body = screens[name]
    if (body == null) {
        println("unknown screen '$name'. available: ${screens.keys.sorted().joinToString(", ")}")
        println("=== UI-INSPECT END ===")
        return
    }

    val scheme = if (dark) DarkColorScheme else LightColorScheme
    runDesktopComposeUiTest(width = 412, height = 915) {
        setContent { body(scheme) }
        waitForIdle()
        dumpTree()
        for (action in actions) {
            println()
            println("--- action: $action ---")
            if (applyAction(action)) {
                waitForIdle()
                dumpTree()
            }
        }
    }
    println("=== UI-INSPECT END ===")
}

/** Walk the merged semantics tree, printing one line per informative node. */
private fun ComposeUiTest.dumpTree() {
    printNode(onRoot().fetchSemanticsNode(), 0)
}

private fun printNode(node: SemanticsNode, depth: Int) {
    val cfg = node.config
    val texts = buildList {
        cfg.getOrNull(SemanticsProperties.Text)?.forEach { add(it.text) }
        cfg.getOrNull(SemanticsProperties.EditableText)?.let { add(it.text) }
        cfg.getOrNull(SemanticsProperties.ContentDescription)?.forEach { add(it) }
    }.filter { it.isNotBlank() }
    val role = cfg.getOrNull(SemanticsProperties.Role)?.toString()
    val clickable = cfg.getOrNull(SemanticsActions.OnClick) != null
    val disabled = cfg.contains(SemanticsProperties.Disabled)

    // Print only informative nodes (carry text, a role, or a click) so the tree stays
    // readable; structural layout nodes are walked but not printed.
    if (texts.isNotEmpty() || role != null || clickable) {
        val b = node.boundsInRoot
        val label = if (texts.isNotEmpty()) "“${texts.joinToString(" / ")}”" else "·"
        val tags = buildList {
            if (role != null) add(role.lowercase())
            if (clickable) add("clickable")
            if (disabled) add("disabled")
        }
        val bounds = "[${b.left.toInt()},${b.top.toInt()} ${(b.right - b.left).toInt()}×${(b.bottom - b.top).toInt()}]"
        val line = listOfNotNull(
            label,
            tags.takeIf { it.isNotEmpty() }?.joinToString(",", "(", ")"),
            bounds,
        ).joinToString(" ")
        println("  ".repeat(depth) + line)
    }
    node.children.forEach { printNode(it, depth + 1) }
}

/** Apply one action: `click:<text>` / `tap:<text>` (substring match) or `dump`. */
private fun ComposeUiTest.applyAction(action: String): Boolean {
    val verb = action.substringBefore(":").trim()
    val arg = action.substringAfter(":", "").trim()
    return when (verb) {
        "click", "tap" -> runCatching {
            onNodeWithText(arg, substring = true).performClick()
        }.fold(
            onSuccess = { true },
            onFailure = {
                println("  ! no clickable node with text containing '$arg'")
                false
            },
        )

        "dump" -> true

        else -> {
            println("  ! unknown action verb '$verb' (use click:<text> or dump)")
            false
        }
    }
}
