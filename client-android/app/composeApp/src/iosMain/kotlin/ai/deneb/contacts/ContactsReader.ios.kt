package ai.deneb.contacts

actual class ContactsReader actual constructor() {
    actual fun isSupported(): Boolean = false
    actual fun hasAccess(): Boolean = false
    actual suspend fun readAll(): List<ContactData> = emptyList()
}
