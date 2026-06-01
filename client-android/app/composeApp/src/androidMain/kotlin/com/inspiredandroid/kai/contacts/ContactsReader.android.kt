package com.inspiredandroid.kai.contacts

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds.Email
import android.provider.ContactsContract.CommonDataKinds.Organization
import android.provider.ContactsContract.CommonDataKinds.Phone
import androidx.core.content.ContextCompat
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.koin.java.KoinJavaComponent.inject

// READ_CONTACTS is declared only in the foss flavor's manifest (privacy parity
// with SMS); playStore omits it. Compile-time per flavor, safe to cache. Mirrors
// NotificationReader's declaresNotificationListener gating.
internal fun Context.declaresReadContacts(): Boolean = try {
    val info = packageManager.getPackageInfo(packageName, PackageManager.GET_PERMISSIONS)
    info.requestedPermissions?.contains(Manifest.permission.READ_CONTACTS) == true
} catch (_: Exception) {
    false
}

actual class ContactsReader actual constructor() {
    private val context: Context by inject(Context::class.java)
    private val supported: Boolean by lazy { context.declaresReadContacts() }

    actual fun isSupported(): Boolean = supported

    actual fun hasAccess(): Boolean = supported &&
        ContextCompat.checkSelfPermission(context, Manifest.permission.READ_CONTACTS) ==
        PackageManager.PERMISSION_GRANTED

    actual suspend fun readAll(): List<ContactData> {
        if (!hasAccess()) return emptyList()
        return withContext(Dispatchers.IO) { queryContacts(context) }
    }
}

// Mutable accumulator while the three data queries are merged by contact id.
private class ContactAcc {
    var name: String = ""
    val phones = LinkedHashSet<String>()
    val emails = LinkedHashSet<String>()
    var org: String = ""
}

// Reads phones, emails, and organizations in three fixed queries (not N+1) and
// merges them by contact id. Contacts without a name are dropped: the gateway
// matches on name, so a nameless entry could never enrich a wiki person anyway.
private fun queryContacts(context: Context): List<ContactData> {
    val byId = LinkedHashMap<Long, ContactAcc>()
    val cr = context.contentResolver

    cr.query(
        Phone.CONTENT_URI,
        arrayOf(Phone.CONTACT_ID, Phone.DISPLAY_NAME, Phone.NUMBER),
        null, null, null,
    )?.use { c ->
        val idCol = c.getColumnIndexOrThrow(Phone.CONTACT_ID)
        val nameCol = c.getColumnIndexOrThrow(Phone.DISPLAY_NAME)
        val numCol = c.getColumnIndexOrThrow(Phone.NUMBER)
        while (c.moveToNext()) {
            val acc = byId.getOrPut(c.getLong(idCol)) { ContactAcc() }
            if (acc.name.isEmpty()) acc.name = c.getString(nameCol).orEmpty().trim()
            c.getString(numCol)?.trim()?.takeIf { it.isNotEmpty() }?.let { acc.phones += it }
        }
    }

    cr.query(
        Email.CONTENT_URI,
        arrayOf(Email.CONTACT_ID, Email.DISPLAY_NAME, Email.ADDRESS),
        null, null, null,
    )?.use { c ->
        val idCol = c.getColumnIndexOrThrow(Email.CONTACT_ID)
        val nameCol = c.getColumnIndexOrThrow(Email.DISPLAY_NAME)
        val addrCol = c.getColumnIndexOrThrow(Email.ADDRESS)
        while (c.moveToNext()) {
            val acc = byId.getOrPut(c.getLong(idCol)) { ContactAcc() }
            if (acc.name.isEmpty()) acc.name = c.getString(nameCol).orEmpty().trim()
            c.getString(addrCol)?.trim()?.takeIf { it.isNotEmpty() }?.let { acc.emails += it }
        }
    }

    cr.query(
        ContactsContract.Data.CONTENT_URI,
        arrayOf(Organization.CONTACT_ID, Organization.COMPANY),
        "${ContactsContract.Data.MIMETYPE} = ?",
        arrayOf(Organization.CONTENT_ITEM_TYPE),
        null,
    )?.use { c ->
        val idCol = c.getColumnIndexOrThrow(Organization.CONTACT_ID)
        val companyCol = c.getColumnIndexOrThrow(Organization.COMPANY)
        while (c.moveToNext()) {
            val acc = byId[c.getLong(idCol)] ?: continue
            if (acc.org.isEmpty()) acc.org = c.getString(companyCol).orEmpty().trim()
        }
    }

    return byId.values.mapNotNull { acc ->
        if (acc.name.isEmpty()) return@mapNotNull null
        ContactData(
            name = acc.name,
            phones = acc.phones.toList(),
            emails = acc.emails.toList(),
            org = acc.org,
        )
    }
}
