package ai.deneb.contacts

import android.Manifest
import android.content.ContentProviderOperation
import android.content.Context
import android.content.pm.PackageManager
import android.provider.ContactsContract
import android.provider.ContactsContract.AggregationExceptions
import android.provider.ContactsContract.CommonDataKinds.Email
import android.provider.ContactsContract.CommonDataKinds.Phone
import androidx.core.content.ContextCompat
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.koin.java.KoinJavaComponent.inject

// WRITE_CONTACTS is declared only in the foss flavor's manifest (privacy parity,
// mirrors READ_CONTACTS); playStore omits it. Compile-time per flavor.
internal fun Context.declaresWriteContacts(): Boolean = try {
    val info = packageManager.getPackageInfo(packageName, PackageManager.GET_PERMISSIONS)
    info.requestedPermissions?.contains(Manifest.permission.WRITE_CONTACTS) == true
} catch (_: Exception) {
    false
}

actual class ContactsWriter actual constructor() {
    private val context: Context by inject(Context::class.java)
    private val supported: Boolean by lazy { context.declaresWriteContacts() }

    actual fun isSupported(): Boolean = supported

    actual fun hasAccess(): Boolean = supported &&
        ContextCompat.checkSelfPermission(context, Manifest.permission.WRITE_CONTACTS) ==
        PackageManager.PERMISSION_GRANTED

    actual suspend fun linkMergeMembers(members: List<ContactParty>): Int {
        if (!hasAccess()) return 0
        return withContext(Dispatchers.IO) { linkRawContactsForMembers(context, members) }
    }

    actual suspend fun linkByIdentity(phones: List<String>, emails: List<String>): Int {
        if (!hasAccess()) return 0
        return withContext(Dispatchers.IO) { linkRawContacts(context, phones, emails) }
    }
}

// Full national digits so apply-time matching stays aligned with the gateway
// contacts mirror (normalizePhone). Last-8 suffix matching would link unrelated
// contacts that merely share a trailing digit run.
private fun normPhone(s: String): String {
    val d = s.filter { it.isDigit() }
    return if (d.startsWith("82") && d.length > 2) "0" + d.substring(2) else d
}

private fun rawContactIdsForMember(context: Context, member: ContactParty): Set<Long> {
    val targetName = normalizePersonName(member.name)
    if (targetName.isEmpty()) return emptySet()
    val phoneKeys = member.phones.map(::normPhone).filter { it.isNotEmpty() }.toSet()
    val emailKeys = member.emails.map { it.trim().lowercase() }.filter { it.isNotEmpty() }.toSet()
    if (phoneKeys.isEmpty() && emailKeys.isEmpty()) return emptySet()
    val ids = LinkedHashSet<Long>()
    val cr = context.contentResolver
    if (phoneKeys.isNotEmpty()) {
        cr.query(Phone.CONTENT_URI, arrayOf(ContactsContract.Data.RAW_CONTACT_ID, Phone.NUMBER, Phone.DISPLAY_NAME), null, null, null)?.use { c ->
            val idCol = c.getColumnIndexOrThrow(ContactsContract.Data.RAW_CONTACT_ID)
            val numCol = c.getColumnIndexOrThrow(Phone.NUMBER)
            val nameCol = c.getColumnIndexOrThrow(Phone.DISPLAY_NAME)
            while (c.moveToNext()) {
                val n = c.getString(numCol)?.let(::normPhone).orEmpty()
                if (n.isEmpty() || n !in phoneKeys) continue
                if (normalizePersonName(c.getString(nameCol).orEmpty()) != targetName) continue
                ids += c.getLong(idCol)
            }
        }
    }
    if (emailKeys.isNotEmpty()) {
        cr.query(Email.CONTENT_URI, arrayOf(ContactsContract.Data.RAW_CONTACT_ID, Email.ADDRESS, Email.DISPLAY_NAME), null, null, null)?.use { c ->
            val idCol = c.getColumnIndexOrThrow(ContactsContract.Data.RAW_CONTACT_ID)
            val addrCol = c.getColumnIndexOrThrow(Email.ADDRESS)
            val nameCol = c.getColumnIndexOrThrow(Email.DISPLAY_NAME)
            while (c.moveToNext()) {
                val e = c.getString(addrCol)?.trim()?.lowercase().orEmpty()
                if (e.isEmpty() || e !in emailKeys) continue
                if (normalizePersonName(c.getString(nameCol).orEmpty()) != targetName) continue
                ids += c.getLong(idCol)
            }
        }
    }
    return ids
}

// Link only raw contacts that match each member card's own name+identifiers.
private fun linkRawContactsForMembers(context: Context, members: List<ContactParty>): Int {
    if (members.size < 2) return 0
    val rawIds = LinkedHashSet<Long>()
    for (member in members) {
        rawIds += rawContactIdsForMember(context, member)
    }
    return linkRawContactIds(context, rawIds.toList())
}

// Every raw-contact id that carries one of the target phones/emails. Two fixed
// queries (not N+1), matched in-code since stored number formats vary.
private fun rawContactIdsForIdentity(context: Context, phones: Set<String>, emails: Set<String>): Set<Long> {
    val ids = LinkedHashSet<Long>()
    val cr = context.contentResolver
    if (phones.isNotEmpty()) {
        cr.query(Phone.CONTENT_URI, arrayOf(ContactsContract.Data.RAW_CONTACT_ID, Phone.NUMBER), null, null, null)?.use { c ->
            val idCol = c.getColumnIndexOrThrow(ContactsContract.Data.RAW_CONTACT_ID)
            val numCol = c.getColumnIndexOrThrow(Phone.NUMBER)
            while (c.moveToNext()) {
                val n = c.getString(numCol)?.let(::normPhone).orEmpty()
                if (n.isNotEmpty() && n in phones) ids += c.getLong(idCol)
            }
        }
    }
    if (emails.isNotEmpty()) {
        cr.query(Email.CONTENT_URI, arrayOf(ContactsContract.Data.RAW_CONTACT_ID, Email.ADDRESS), null, null, null)?.use { c ->
            val idCol = c.getColumnIndexOrThrow(ContactsContract.Data.RAW_CONTACT_ID)
            val addrCol = c.getColumnIndexOrThrow(Email.ADDRESS)
            while (c.moveToNext()) {
                val e = c.getString(addrCol)?.trim()?.lowercase().orEmpty()
                if (e.isNotEmpty() && e in emails) ids += c.getLong(idCol)
            }
        }
    }
    return ids
}

// KEEP_TOGETHER is pairwise, so linking each subsequent raw contact to the first
// aggregates the whole set into one contact. Reversible: a KEEP_SEPARATE unlinks.
private fun linkRawContacts(context: Context, phones: List<String>, emails: List<String>): Int {
    val phoneKeys = phones.map(::normPhone).filter { it.isNotEmpty() }.toSet()
    val emailKeys = emails.map { it.trim().lowercase() }.filter { it.isNotEmpty() }.toSet()
    if (phoneKeys.isEmpty() && emailKeys.isEmpty()) return 0
    val rawIds = rawContactIdsForIdentity(context, phoneKeys, emailKeys).toList()
    return linkRawContactIds(context, rawIds)
}

private fun linkRawContactIds(context: Context, rawIds: List<Long>): Int {
    if (rawIds.size < 2) return 0
    val ops = ArrayList<ContentProviderOperation>()
    for (i in 1 until rawIds.size) {
        ops += ContentProviderOperation.newUpdate(AggregationExceptions.CONTENT_URI)
            .withValue(AggregationExceptions.TYPE, AggregationExceptions.TYPE_KEEP_TOGETHER)
            .withValue(AggregationExceptions.RAW_CONTACT_ID1, rawIds[0])
            .withValue(AggregationExceptions.RAW_CONTACT_ID2, rawIds[i])
            .build()
    }
    return try {
        context.contentResolver.applyBatch(ContactsContract.AUTHORITY, ops)
        rawIds.size
    } catch (_: Exception) {
        0
    }
}
