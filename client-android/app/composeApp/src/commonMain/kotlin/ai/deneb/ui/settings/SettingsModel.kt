package ai.deneb.ui.settings

import androidx.compose.runtime.Immutable
import org.jetbrains.compose.resources.StringResource

/**
 * A selectable model row for a configured service. Lives in [ai.deneb.ui.settings]
 * for historical reasons: [ai.deneb.data.ModelTransformations] maps provider model
 * lists into this shape. The Kai-style settings UI that originally rendered it was
 * removed (it was unreachable), but this transport type is still live, so it was
 * extracted here on its own.
 */
@Immutable
data class SettingsModel(
    val id: String,
    val subtitle: String,
    val description: String? = null,
    val descriptionRes: StringResource? = null,
    val isSelected: Boolean = false,
    /** Human-readable name to display in place of [id], when the provider exposes one. */
    val displayName: String? = null,
    /** Max context window in tokens, from the API or the curated catalog. */
    val contextWindow: Long? = null,
    /** Release date as "YYYY-MM" or "YYYY-MM-DD", from the API or the curated catalog. */
    val releaseDate: String? = null,
    /** Parameter count, pre-formatted for display (e.g. "70B", "8B", "3.3B"). */
    val parameterCount: String? = null,
    /** LMArena Elo score, or null when unknown. */
    val arenaScore: Int? = null,
)
