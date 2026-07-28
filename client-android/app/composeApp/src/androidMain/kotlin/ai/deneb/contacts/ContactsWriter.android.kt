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
import java.io.File

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

    actual suspend fun linkByIdentity(phones: List<String>, emails: List<String>, names: List<String>): Int {
        if (!hasAccess()) return 0
        return withContext(Dispatchers.IO) { linkRawContacts(context, phones, emails, names) }
    }

    actual suspend fun countMergeLinks(): Int {
        if (!hasAccess()) return 0
        return withContext(Dispatchers.IO) { readMergeLinks(context).size }
    }

    actual suspend fun resetMergeLinks(): ContactsResetResult {
        if (!hasAccess()) return ContactsResetResult()
        return withContext(Dispatchers.IO) {
            val pairs = readMergeLinks(context)
            if (pairs.isEmpty()) {
                ContactsResetResult()
            } else {
                // Back up before undoing: once a pair returns to AUTOMATIC the provider
                // forgets it, and the pairs cannot be recomputed afterwards.
                val backup = writeMergeBackup(context, pairs)
                ContactsResetResult(undone = clearMergeLinks(context, pairs), backupPath = backup)
            }
        }
    }
}

private class MergePair(val id1: Long, val id2: Long)

// Every forced merge currently on the device. KEEP_SEPARATE rows are skipped on
// purpose: those say "never merge these two", so clearing them would create merges
// rather than undo them.
private fun readMergeLinks(context: Context): List<MergePair> {
    val out = ArrayList<MergePair>()
    context.contentResolver.query(
        AggregationExceptions.CONTENT_URI,
        arrayOf(
            AggregationExceptions.TYPE,
            AggregationExceptions.RAW_CONTACT_ID1,
            AggregationExceptions.RAW_CONTACT_ID2,
        ),
        null,
        null,
        null,
    )?.use { c ->
        val typeCol = c.getColumnIndexOrThrow(AggregationExceptions.TYPE)
        val id1Col = c.getColumnIndexOrThrow(AggregationExceptions.RAW_CONTACT_ID1)
        val id2Col = c.getColumnIndexOrThrow(AggregationExceptions.RAW_CONTACT_ID2)
        while (c.moveToNext()) {
            if (c.getInt(typeCol) == AggregationExceptions.TYPE_KEEP_TOGETHER) {
                out += MergePair(c.getLong(id1Col), c.getLong(id2Col))
            }
        }
    }
    return out
}

private fun writeMergeBackup(context: Context, pairs: List<MergePair>): String = try {
    val file = File(context.filesDir, "contacts-merge-backup-${System.currentTimeMillis()}.json")
    file.writeText(pairs.joinToString(",", "[", "]") { "{\"a\":${it.id1},\"b\":${it.id2}}" })
    file.absolutePath
} catch (_: Exception) {
    ""
}

// TYPE_AUTOMATIC hands the pair back to Android's own aggregation — the pre-apply
// state. Chunked because the provider re-aggregates on every apply, so one huge
// batch stalls; and a chunk that fails (e.g. a raw contact was since deleted) must
// not strand the rest.
private fun clearMergeLinks(context: Context, pairs: List<MergePair>): Int {
    var undone = 0
    pairs.chunked(64).forEach { chunk ->
        val ops = ArrayList<ContentProviderOperation>(chunk.size)
        chunk.forEach { pair ->
            ops += ContentProviderOperation.newUpdate(AggregationExceptions.CONTENT_URI)
                .withValue(AggregationExceptions.TYPE, AggregationExceptions.TYPE_AUTOMATIC)
                .withValue(AggregationExceptions.RAW_CONTACT_ID1, pair.id1)
                .withValue(AggregationExceptions.RAW_CONTACT_ID2, pair.id2)
                .build()
        }
        try {
            context.contentResolver.applyBatch(ContactsContract.AUTHORITY, ops)
            undone += chunk.size
        } catch (_: Exception) {
            // keep going: partial recovery beats aborting the whole undo
        }
    }
    return undone
}

// Full national digits so apply-time matching stays aligned with the gateway
// contacts mirror (normalizePhone). Last-8 suffix matching would link unrelated
// contacts that merely share a trailing digit run.
// How many device raw-contacts a group may exceed its member count by before the
// link is refused. Small: the group already enumerates the entries the gateway saw,
// and the slack only covers device-only duplicates it could not have seen.
private const val linkFanoutSlack = 3

private fun normPhone(s: String): String {
    val d = s.filter { it.isDigit() }
    return if (d.startsWith("82") && d.length > 2) "0" + d.substring(2) else d
}

// Display name per raw-contact id, for the name gate. One fixed query; the
// aggregate's name is not usable here because the whole point is to decide what
// belongs in the aggregate.
private fun rawContactNames(context: Context): Map<Long, String> {
    val out = HashMap<Long, String>()
    context.contentResolver.query(
        ContactsContract.RawContacts.CONTENT_URI,
        arrayOf(ContactsContract.RawContacts._ID, ContactsContract.RawContacts.DISPLAY_NAME_PRIMARY),
        null,
        null,
        null,
    )?.use { c ->
        val idCol = c.getColumnIndexOrThrow(ContactsContract.RawContacts._ID)
        val nameCol = c.getColumnIndexOrThrow(ContactsContract.RawContacts.DISPLAY_NAME_PRIMARY)
        while (c.moveToNext()) out[c.getLong(idCol)] = c.getString(nameCol).orEmpty()
    }
    return out
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
private fun linkRawContacts(
    context: Context,
    phones: List<String>,
    emails: List<String>,
    names: List<String>,
): Int {
    val phoneKeys = phones.map(::normPhone).filter { it.isNotEmpty() }.toSet()
    val emailKeys = emails.map { it.trim().lowercase() }.filter { it.isNotEmpty() }.toSet()
    if (phoneKeys.isEmpty() && emailKeys.isEmpty()) return 0
    // No usable name for the group means no gate, and an ungated link is the bug
    // that collapsed the book. Refuse rather than fall back to identifiers alone.
    val groupKeys = groupMatchKeys(names)
    if (groupKeys.isEmpty()) return 0
    val byIdentity = rawContactIdsForIdentity(context, phoneKeys, emailKeys)
    val displayNames = rawContactNames(context)
    val rawIds = byIdentity.filter { nameBelongsToGroup(displayNames[it].orEmpty(), groupKeys) }
    if (rawIds.size < 2) return 0
    // Backstop: a real person does not hold many more cards than the group the
    // gateway named. A blow-up here means the gate let something through, so skip
    // the group entirely rather than link a crowd.
    if (rawIds.size > names.size + linkFanoutSlack) return 0
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
