package ai.deneb

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// The NavHost's resting start destination. Renders nothing: the five bottom-bar
// tabs live OUTSIDE the NavHost in an always-alive pane (LiveTabPane, App.kt) that
// shows through this transparent stub, so tab switches never rebuild a screen.
// Pushed routes (sections, details) slide over the pane.
@Serializable
@SerialName("deneb_main")
object DenebMain

@Serializable
@SerialName("home")
object Home

@Serializable
@SerialName("deneb_feed")
data class DenebFeed(
    val openItemId: String? = null,
    val openItemCreatedAtMs: Long = 0L,
)

@Serializable
@SerialName("deneb_config")
object DenebConfig

@Serializable
@SerialName("deneb_fleet")
object DenebFleet

@Serializable
@SerialName("deneb_mail")
object DenebMail

@Serializable
@SerialName("deneb_calendar")
object DenebCalendar

@Serializable
@SerialName("deneb_mail_detail")
data class DenebMailDetail(val id: String)

@Serializable
@SerialName("deneb_calendar_event")
data class DenebCalendarEvent(val id: String)

@Serializable
@SerialName("deneb_calendar_add")
data class DenebCalendarAdd(val dateIso: String)

@Serializable
@SerialName("deneb_calendar_edit")
data class DenebCalendarEdit(val id: String)

@Serializable
@SerialName("deneb_todo_add")
data class DenebTodoAdd(val dueIso: String? = null)

@Serializable
@SerialName("deneb_todo_edit")
data class DenebTodoEdit(val id: String)

@Serializable
@SerialName("deneb_search")
object DenebSearch

// 더보기 — the section hub (bottom-bar tab). A grouped text list of the sections that are
// not first-class bottom-bar tabs (파트별 업무 현황·조직도·검색·할일·일기·카테고리·전체 연락처·
// 노트북·파일·브라우저·설정). 채팅·피드·메일·달력 are their own tabs. See DenebMoreScreen.
@Serializable
@SerialName("deneb_more")
object DenebMore

@Serializable
@SerialName("deneb_wiki")
data class DenebWiki(val path: String)

@Serializable
@SerialName("deneb_people")
object DenebPeople

@Serializable
@SerialName("deneb_approvals")
object DenebApprovals

@Serializable
@SerialName("deneb_approval_detail")
data class DenebApprovalDetail(
    val docId: String,
    val title: String = "",
    val drafter: String = "",
    val date: String = "",
    val canAct: Boolean = false,
    // List row's box (pending|done|cc|total) — reader folder hint for a fast cold open.
    val folder: String = "",
)

@Serializable
@SerialName("deneb_groupware")
object DenebGroupware

@Serializable
@SerialName("deneb_person")
data class DenebPerson(val sender: String)

// Full address book (전체 연락처) — the raw contacts.json mirror, sectioned ㄱㄴㄷ.
// Distinct from DenebPeople (Gmail counterparties + 인물 wiki, volume-ranked).
@Serializable
@SerialName("deneb_contacts")
object DenebContacts

@Serializable
@SerialName("deneb_categories")
object DenebCategories

@Serializable
@SerialName("deneb_diary")
object DenebDiary

@Serializable
@SerialName("deneb_notebooks")
// openId deep-links straight into one notebook's detail (e.g. from a wiki project
// page's "이 딜 노트북" link); null opens the notebook list.
data class DenebNotebooks(val openId: String? = null)

@Serializable
@SerialName("deneb_dashboard")
object DenebDashboard

@Serializable
@SerialName("deneb_rsi")
object DenebRsi

@Serializable
@SerialName("deneb_usage")
object DenebUsage

@Serializable
@SerialName("deneb_project_digests")
object DenebProjectDigests

@Serializable
@SerialName("deneb_site_map")
object DenebSiteMap

@Serializable
@SerialName("deneb_org")
object DenebOrgChart

@Serializable
@SerialName("deneb_category_pages")
data class DenebCategoryPages(val category: String)

@Serializable
@SerialName("deneb_skill")
data class DenebSkill(val name: String)

@Serializable
@SerialName("deneb_browser")
data class DenebBrowser(val url: String)

@Serializable
@SerialName("deneb_cron")
data class DenebCron(val cronId: String)

@Serializable
@SerialName("deneb_cron_edit")
data class DenebCronEdit(val cronId: String)

@Serializable
@SerialName("deneb_files")
object DenebFiles
