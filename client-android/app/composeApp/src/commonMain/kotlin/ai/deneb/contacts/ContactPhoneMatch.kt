package ai.deneb.contacts

/**
 * Normalizes a phone for identity matching — mirrors gateway-go
 * `contacts.normalizePhone` (+82 country code → national 0 prefix). Used when
 * linking device raw contacts so format variants of the same number match without
 * collapsing unrelated numbers that merely share trailing digits.
 */
fun normalizePhoneForIdentityMatch(raw: String): String {
    val digits = buildString {
        raw.forEach { c ->
            if (c in '0'..'9') append(c)
        }
    }
    return if (digits.startsWith("82") && digits.length > 2) {
        "0" + digits.substring(2)
    } else {
        digits
    }
}

/** Personal-phone gate — same ≥9-digit rule as gateway dedup. */
fun isPersonalPhoneKey(normalized: String): Boolean = normalized.length >= 9
