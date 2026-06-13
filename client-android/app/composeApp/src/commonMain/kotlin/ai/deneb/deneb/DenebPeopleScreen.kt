package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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

/**
 * The merged people surface (`miniapp.people.list`), reached from the categories
 * screen's pinned "사람" row: recent Gmail counterparties ranked by message volume
 * ("최근 연락", with their 인물 wiki summary inline when matched) followed by
 * wiki-only people with no recent mail ("인물 위키"). Tapping a contact opens the
 * person dossier; tapping a wiki-only person opens their 인물 page directly.
 * Framed by [DenebScreenScaffold].
 */
@Composable
fun DenebPeopleScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenPerson: (String) -> Unit = {},
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var people by remember { mutableStateOf<List<PersonHit>?>(null) }
    var failed by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    // people: null = first load in flight, list = loaded. failed takes priority so a
    // fetch error offers retry instead of the misleading "no contacts" empty line.
    suspend fun load() {
        failed = false
        people = null
        val fetched = client.fetchPeople()
        if (fetched == null) failed = true else people = fetched
    }
    LaunchedEffect(Unit) { load() }

    // Sub-screen of categories on every platform (no drawer/sidebar entry of its
    // own), so the back affordance stays on desktop too.
    DenebScreenScaffold(title = "사람", onBack = onBack, tabBar = navigationTabBar) {
        val list = people
        when {
            failed -> DenebError(
                "사람 목록을 불러오지 못했습니다.",
                onRetry = { scope.launch { load() } },
            )

            list == null -> DenebLoading()

            list.isEmpty() -> DenebEmpty("표시할 사람이 없습니다.")

            else -> {
                // Recent Gmail counterparties vs. wiki-only people (no recent
                // mail). The gateway appends the wiki-only block, but partition
                // here so each section is labeled and keyed independently.
                val (contacts, wikiOnly) = list.partition { it.email.isNotBlank() || it.messageCount > 0 }
                LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
                    if (contacts.isNotEmpty()) {
                        item(key = "h:contacts") {
                            DenebSectionLabel("최근 연락", Modifier.padding(horizontal = 16.dp))
                        }
                        items(contacts, key = { "p:" + it.email.ifBlank { it.name } }) { person ->
                            ContactPersonRow(
                                person = person,
                                onTap = {
                                    haptics.tap()
                                    onOpenPerson(person.email.ifBlank { person.name })
                                },
                                modifier = Modifier.animateItem(),
                            )
                        }
                    }
                    if (wikiOnly.isNotEmpty()) {
                        item(key = "h:wiki") {
                            DenebSectionLabel("인물 위키", Modifier.padding(horizontal = 16.dp))
                        }
                        items(wikiOnly, key = { "w:" + it.wikiPath.ifBlank { it.name } }) { person ->
                            WikiPersonRow(
                                person = person,
                                onTap = {
                                    haptics.tap()
                                    if (person.wikiPath.isNotBlank()) {
                                        onOpenWiki(person.wikiPath)
                                    } else {
                                        onOpenPerson(person.name)
                                    }
                                },
                                modifier = Modifier.animateItem(),
                            )
                        }
                    }
                }
            }
        }
    }
}

/** A recent counterparty: name + message count, with the 인물 wiki summary as the
 *  subtitle when this sender is matched to a page (else the last mail subject). */
@Composable
private fun ContactPersonRow(person: PersonHit, onTap: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier
            .fillMaxWidth()
            .denebPressable(onClick = onTap)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                person.name.ifBlank { "(이름 없음)" },
                style = MaterialTheme.typography.bodyLarge,
                fontWeight = FontWeight.Medium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                "${person.messageCount}통",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
            )
        }
        val subtitle = person.wikiSummary.ifBlank { person.lastSubject }
        if (subtitle.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
    HorizontalDivider(
        modifier = Modifier.padding(start = 16.dp),
        color = denebHairline(),
    )
}

/** An 인물 wiki person with no recent mail: name + page summary, no count. */
@Composable
private fun WikiPersonRow(person: PersonHit, onTap: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier
            .fillMaxWidth()
            .denebPressable(onClick = onTap)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Text(
            person.name.ifBlank { "(이름 없음)" },
            style = MaterialTheme.typography.bodyLarge,
            fontWeight = FontWeight.Medium,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (person.wikiSummary.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(
                person.wikiSummary,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
    HorizontalDivider(
        modifier = Modifier.padding(start = 16.dp),
        color = denebHairline(),
    )
}
