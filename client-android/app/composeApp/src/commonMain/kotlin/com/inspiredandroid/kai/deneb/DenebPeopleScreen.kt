package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import com.inspiredandroid.kai.ui.components.rememberHaptics
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
 * People ranked by recent message volume (`miniapp.people.list`). Tapping a
 * person opens their relationship context. Surface-wrapped for dark mode.
 */
@Composable
fun DenebPeopleScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenPerson: (String) -> Unit = {},
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

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            if (navigationTabBar != null) {
                Spacer(Modifier.height(8.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "사람",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onBack) { Text("닫기") }
            }

            val list = people
            when {
                failed -> DenebError(
                    "연락처를 불러오지 못했어요.",
                    onRetry = { scope.launch { load() } },
                )
                list == null -> DenebLoading()
                list.isEmpty() -> DenebEmpty("최근 연락이 없습니다.")
                else -> LazyColumn(Modifier.fillMaxSize()) {
                    items(list, key = { it.email.ifBlank { it.name } }) { person ->
                        Column(
                            Modifier
                                .animateItem()
                                .fillMaxWidth()
                                .clickable { haptics.tap(); onOpenPerson(person.email.ifBlank { person.name }) }
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
                            if (person.lastSubject.isNotBlank()) {
                                Spacer(Modifier.height(2.dp))
                                Text(
                                    person.lastSubject,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 16.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                        )
                    }
                }
            }
        }
    }
}
