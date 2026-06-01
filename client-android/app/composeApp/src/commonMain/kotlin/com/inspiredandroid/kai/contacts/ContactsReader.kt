package com.inspiredandroid.kai.contacts

/**
 * One address-book entry shipped to the gateway's `miniapp.capture.contacts` RPC.
 * The field shape matches the gateway's wiki.Contact: name + phones + emails + org.
 */
data class ContactData(
    val name: String,
    val phones: List<String> = emptyList(),
    val emails: List<String> = emptyList(),
    val org: String = "",
)

/**
 * Reads the device address book. Only the Android actual returns real data — gated
 * by READ_CONTACTS being declared (the foss flavor) and granted at runtime. Other
 * platforms return empty, and [isSupported] is false so the UI hides the feature.
 */
expect class ContactsReader() {
    /** True when this build can read contacts at all (Android + permission declared). */
    fun isSupported(): Boolean

    /** True when [isSupported] and the user has granted READ_CONTACTS. */
    fun hasAccess(): Boolean

    /** Every contact that has a name, with phones/emails/org merged per person. */
    suspend fun readAll(): List<ContactData>
}
