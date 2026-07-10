package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

// Org node/member editors (bottom-sheet forms) — split from
// DenebOrgChartScreen.kt (pure move).

/**
 * The node editor: rename + type picker + dashboard-part (lane) toggle + member CRUD
 * (each with 직급/직책 dropdowns, plus read-only call/email shortcuts when the member is
 * matched in the contacts store) + delete. Every editable control writes back through
 * [onChange] with the updated node, so the parent's working tree stays the single
 * source of truth (no local mirror to desync); the contact shortcuts are display-only
 * (numbers live in the contacts store, never in org.json). Stateless over its node —
 * previewable.
 */
@Composable
internal fun OrgNodeEditor(
    node: OrgNodeOut,
    onChange: (OrgNodeOut) -> Unit,
    onDelete: () -> Unit,
    onDone: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(start = 20.dp, end = 20.dp, bottom = 24.dp),
    ) {
        Text("조직 편집", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        Spacer(Modifier.height(12.dp))

        // Name.
        OrgFieldLabel("이름")
        DenebOutlinedTextField(
            value = node.name,
            onValueChange = { onChange(node.copy(name = it)) },
            placeholder = { Text("예: 기획조정실 1팀") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(14.dp))

        // Type picker.
        OrgFieldLabel("종류")
        OrgEnumDropdown(
            value = orgTypeLabel(node.type),
            options = orgTypes,
            optionLabel = ::orgTypeLabel,
            placeholder = "종류 선택",
            onSelect = { onChange(node.copy(type = it)) },
        )
        Spacer(Modifier.height(14.dp))

        // Dashboard-part (lane) toggle. A tagged node becomes a 파트별 업무 현황 column.
        // The lane *key* is an internal id (we seed it from the node id); the operator
        // only chooses on/off, so no raw key is ever shown.
        //
        // Toggling off clears the lane to ""; toggling back on must restore the SAME
        // key it had, not re-seed from the node id — otherwise a hand-edited
        // meaningful key (e.g. a chart authored off-app with lane "sales") is lost on
        // an off→on round-trip. Remember the last non-blank lane (per node id) and
        // prefer it when re-enabling, falling back to the node id only when there was
        // never a prior key.
        var lastLane by remember(node.id) { mutableStateOf(node.lane) }
        if (node.lane.isNotBlank()) lastLane = node.lane
        OrgPartToggle(
            on = node.lane.isNotBlank(),
            onToggle = { on ->
                onChange(node.copy(lane = if (on) lastLane.ifBlank { node.id } else ""))
            },
        )
        Spacer(Modifier.height(18.dp))

        // Classification rules — shown only for a 파트 node. Keywords (도메인 용어) and
        // 거래처 (counterparty names) route work items to this part's dashboard column;
        // member names are the strong signal, these are weak/medium. Edited as
        // comma-separated lists so the operator never sees a raw key or array syntax.
        if (node.lane.isNotBlank()) {
            OrgFieldLabel("분류 키워드 (쉼표로 구분)")
            DenebOutlinedTextField(
                value = node.keywords.joinToString(", "),
                onValueChange = { onChange(node.copy(keywords = splitCsv(it))) },
                placeholder = { Text("예: 태양광, 모듈, 인버터") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            OrgFieldLabel("분류 거래처 (쉼표로 구분)")
            DenebOutlinedTextField(
                value = node.companies.joinToString(", "),
                onValueChange = { onChange(node.copy(companies = splitCsv(it))) },
                placeholder = { Text("예: 트리나솔라, 한화") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(18.dp))
        }

        // Members.
        OrgFieldLabel("구성원")
        if (node.members.isEmpty()) {
            Text("아직 구성원이 없습니다.", style = DenebType.rowSubtitle, color = denebHint(), modifier = Modifier.padding(vertical = 4.dp))
        }
        node.members.forEachIndexed { idx, member ->
            OrgMemberEditor(
                member = member,
                onChange = { updated ->
                    onChange(node.copy(members = node.members.toMutableList().also { it[idx] = updated }))
                },
                onRemove = {
                    onChange(node.copy(members = node.members.toMutableList().also { it.removeAt(idx) }))
                },
            )
            Spacer(Modifier.height(8.dp))
        }
        OutlinedButton(
            onClick = { onChange(node.copy(members = node.members + MemberOut(name = ""))) },
            modifier = Modifier.fillMaxWidth(),
        ) {
            Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(6.dp))
            Text("구성원 추가")
        }
        Spacer(Modifier.height(22.dp))

        // Actions: delete (left) + done (right). Delete drops the node and its subtree
        // from the working tree (parent's onDelete); 저장 is the screen-level header
        // button — done just closes the sheet so the edits stay in the working tree.
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onDelete) {
                Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("조직 삭제", color = MaterialTheme.colorScheme.error)
            }
            FilledTonalButton(onClick = onDone) { Text("완료") }
        }
    }
}

/** One member's editable row: name field + 직급 dropdown + 직책 dropdown + remove. */
@Composable
private fun OrgMemberEditor(
    member: MemberOut,
    onChange: (MemberOut) -> Unit,
    onRemove: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            DenebOutlinedTextField(
                value = member.name,
                onValueChange = { onChange(member.copy(name = it)) },
                placeholder = { Text("이름") },
                singleLine = true,
                modifier = Modifier.weight(1f),
            )
            IconButton(onClick = onRemove, modifier = Modifier.size(40.dp)) {
                Icon(Icons.Outlined.Delete, contentDescription = "구성원 삭제", tint = denebHint(), modifier = Modifier.size(18.dp))
            }
        }
        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.weight(1f)) {
                OrgEnumDropdown(
                    value = member.rank,
                    options = orgRanks,
                    optionLabel = { it },
                    placeholder = "직급",
                    allowClear = true,
                    onSelect = { onChange(member.copy(rank = it)) },
                )
            }
            Box(Modifier.weight(1f)) {
                OrgEnumDropdown(
                    value = member.position,
                    options = orgPositions,
                    optionLabel = { it },
                    placeholder = "직책",
                    allowClear = true,
                    onSelect = { onChange(member.copy(position = it)) },
                )
            }
        }
        // Contact row: call/email shortcuts from the gateway's read-only enrichment.
        // Only renders for members the contacts store matched (phones/emails present),
        // so the editor stays uncluttered for the rest. Labelled so its purpose is clear
        // next to the editable name/직급/직책 fields above (which never store numbers).
        if (member.phones.any { it.isNotBlank() } || member.emails.any { it.isNotBlank() }) {
            Spacer(Modifier.height(6.dp))
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("연락처", style = DenebType.sectionLabel, color = denebHint())
                OrgContactActions(member = member, glyphSize = 18.dp, leadingGap = 6.dp)
            }
        }
    }
}

/** A small tracked-caps field label above an editor control. */
@Composable
private fun OrgFieldLabel(text: String) {
    Text(text, style = DenebType.sectionLabel, color = denebHint(), modifier = Modifier.padding(bottom = 6.dp))
}

/**
 * A friendly enum picker (Material ExposedDropdownMenuBox) — never exposes the raw
 * value. Shows [value] (already a display label or a plain enum string), lists
 * [options] rendered through [optionLabel], and reports the chosen *raw* option via
 * [onSelect]. With [allowClear] a "(없음)" item clears to empty (for optional fields
 * like 직급/직책).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun OrgEnumDropdown(
    value: String,
    options: List<String>,
    optionLabel: (String) -> String,
    placeholder: String,
    onSelect: (String) -> Unit,
    allowClear: Boolean = false,
) {
    var expanded by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = it },
    ) {
        DenebOutlinedTextField(
            value = value,
            onValueChange = {},
            readOnly = true,
            placeholder = { Text(placeholder) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            if (allowClear) {
                DropdownMenuItem(
                    text = { Text("(없음)", color = denebHint()) },
                    onClick = {
                        onSelect("")
                        expanded = false
                    },
                )
            }
            options.forEach { opt ->
                DropdownMenuItem(
                    text = { Text(optionLabel(opt)) },
                    onClick = {
                        onSelect(opt)
                        expanded = false
                    },
                )
            }
        }
    }
}

/** Dashboard-part toggle row — a chip the operator taps to mark this node a 파트별
 *  업무 현황 column. Selected = warm insight accent (the dashboard's color). */
@Composable
private fun OrgPartToggle(on: Boolean, onToggle: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text("대시보드 파트", style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.onBackground)
            Text(
                "켜면 ‘파트별 업무 현황’에 이 조직의 칸이 생깁니다.",
                style = DenebType.snippet,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        Spacer(Modifier.width(10.dp))
        DenebChip(selected = on, onClick = { onToggle(!on) }) {
            Text(if (on) "파트로 사용 중" else "파트 아님")
        }
    }
}
