package ai.deneb.deneb

import ai.deneb.deneb.generated.ContactRow
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.components.SectionedScrubList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * 전체 연락처 — the full device address book (`miniapp.contacts.list`, contacts.json
 * synced via 연락처 capture). Distinct from the 사람 screen (Gmail counterparties +
 * 인물 wiki, volume-ranked): this is the raw, complete list, sectioned alphabetically
 * (ㄱㄴㄷ/A–Z) with a scrub index via the shared [SectionedScrubList], plus name/
 * number/email search. Tapping a contact dials its first number (else emails the
 * first address). Reached from 더보기. Stateful shell (load + states); the previewable
 * body is [ContactsList].
 */
@Composable
fun DenebContactsScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var contacts by remember { mutableStateOf<List<ContactRow>?>(null) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    // null = first load in flight; failed takes priority so a fetch error offers
    // retry instead of the misleading "no contacts" empty line.
    suspend fun load() {
        failed = false
        contacts = null
        val fetched = client.fetchContacts()
        if (fetched == null) failed = true else contacts = fetched
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "전체 연락처", onBack = onBack, tabBar = navigationTabBar) {
        val list = contacts
        when {
            failed -> DenebError("연락처를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
            list == null -> DenebLoading()
            list.isEmpty() -> DenebEmpty("연락처가 없습니다.")
            else -> ContactsList(list)
        }
    }
}

/** Search + ㄱㄴㄷ-sectioned address book. Stateless over [contacts] so the render
 *  harness can drive it; tapping a row dials/emails (override [onOpen] for previews). */
@Composable
internal fun ContactsList(
    contacts: List<ContactRow>,
    onOpen: (ContactRow) -> Unit = ::openContact,
) {
    val haptics = rememberHaptics()
    var query by remember { mutableStateOf("") }
    val filtered = remember(contacts, query) {
        val q = query.trim()
        if (q.isEmpty()) {
            contacts
        } else {
            contacts.filter { c ->
                c.name.contains(q, ignoreCase = true) ||
                    c.org.contains(q, ignoreCase = true) ||
                    c.phones.any { it.contains(q) } ||
                    c.emails.any { it.contains(q, ignoreCase = true) }
            }
        }
    }
    // Dedup so duplicate address-book entries can't collide LazyColumn keys.
    val unique = remember(filtered) { filtered.distinctBy { contactKey(it) } }
    Column(Modifier.fillMaxSize()) {
        DenebSearchField(
            query = query,
            onQueryChange = { query = it },
            placeholder = "이름·번호·이메일 검색",
            clearContentDescription = "검색 지우기",
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
        if (unique.isEmpty()) {
            DenebEmpty("검색 결과 없음")
        } else {
            SectionedScrubList(
                items = unique,
                label = { it.name },
                key = { contactKey(it) },
            ) { c ->
                ContactRowItem(contact = c, onTap = {
                    haptics.tap()
                    onOpen(c)
                })
            }
        }
    }
}

/** Tap a contact: dial the first phone, else email the first address — the address
 *  book's practical actions. No-op when the contact has neither. */
private fun openContact(c: ContactRow) {
    val phone = c.phones.firstOrNull()?.filter { it.isDigit() || it == '+' }
    if (!phone.isNullOrBlank()) {
        openUrl("tel:$phone")
        return
    }
    c.emails.firstOrNull()?.takeIf { it.isNotBlank() }?.let { openUrl("mailto:$it") }
}

/** Stable, collision-proof identity for an address-book entry (name alone repeats). */
private fun contactKey(c: ContactRow): String = c.name + " " + c.phones.joinToString(",") + " " + c.emails.joinToString(",")

@Composable
private fun ContactRowItem(contact: ContactRow, onTap: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onTap)
            .padding(horizontal = 20.dp, vertical = 11.dp),
    ) {
        Text(
            contact.name,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        val sub = contact.org.ifBlank { contact.phones.firstOrNull() ?: contact.emails.firstOrNull() ?: "" }
        if (sub.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(
                sub,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
