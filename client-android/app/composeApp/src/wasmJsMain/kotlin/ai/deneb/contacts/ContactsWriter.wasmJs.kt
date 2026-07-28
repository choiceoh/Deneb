package ai.deneb.contacts

actual class ContactsWriter actual constructor() {
    actual fun isSupported(): Boolean = false
    actual fun hasAccess(): Boolean = false
    actual suspend fun linkByIdentity(phones: List<String>, emails: List<String>): Int = 0
    actual suspend fun countMergeLinks(): Int = 0
    actual suspend fun resetMergeLinks(): ContactsResetResult = ContactsResetResult()
}
