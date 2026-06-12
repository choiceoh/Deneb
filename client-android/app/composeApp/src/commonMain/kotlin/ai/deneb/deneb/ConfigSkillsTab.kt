package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillRow
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "스킬" tab: skills the agent can use (read-only). Mirrors the
// system-prompt skill list via miniapp.skills.list — name, description, category,
// source, and whether the skill is user-invocable (rendered with a leading slash).
// No toggles: discovery is filesystem-driven, so the list reflects what's
// installed on the gateway host. Hosted by [DenebConfigScreen]'s pager.
@Composable
internal fun SkillsTab(client: DenebGatewayClient) {
    val skills by client.denebSkills.collectAsState()
    val scope = rememberCoroutineScope()
    var loadFailed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { loadFailed = !client.refreshSkills() }
    when {
        skills.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError(
                "스킬을 불러오지 못했습니다.",
                onRetry = { scope.launch { loadFailed = !client.refreshSkills() } },
            )
        }

        skills.isEmpty() -> EmptyTab("사용할 수 있는 스킬이 없습니다.")

        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(skills, key = { it.name }) { skill ->
                Column(
                    Modifier.animateItem().fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
                ) {
                    // Skill name only — no runnable slash command. The live slash
                    // dispatcher matches a lowercased raw name (not a sanitized
                    // command) and only for local/system skills, so showing a
                    // command here would risk advertising one that doesn't route.
                    Text(
                        skill.name,
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (skill.description.isNotBlank()) {
                        Text(
                            skill.description,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    val meta = skillMetaLine(skill)
                    if (meta.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            meta,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
            }
        }
    }
}

// skillSourceLabel maps the gateway's discovery-origin string to a Korean label,
// matching DenebConfigScreen's literal-string convention (this screen doesn't use
// stringResource). Falls back to the raw value for origins we don't surface yet.
private fun skillSourceLabel(source: String): String = when (source) {
    "managed" -> "관리형"
    "workspace" -> "워크스페이스"
    "agents-skills-personal" -> "개인"
    "agents-skills-project" -> "프로젝트"
    "bundled" -> "기본 제공"
    "plugin" -> "플러그인"
    "extra" -> "추가"
    else -> source
}

// skillMetaLine renders "category · source", omitting whichever is blank.
private fun skillMetaLine(skill: SkillRow): String = listOfNotNull(
    skill.category.takeIf { it.isNotBlank() },
    skillSourceLabel(skill.source).takeIf { it.isNotBlank() },
).joinToString(" · ")
