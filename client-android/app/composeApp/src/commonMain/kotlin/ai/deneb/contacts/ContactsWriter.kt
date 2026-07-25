package ai.deneb.contacts

/**
 * Applies the address-book dedup to the device by LINKING duplicate raw contacts
 * into one aggregated contact — Android's own merge (AggregationExceptions,
 * KEEP_TOGETHER). It never deletes data, so it is reversible (unlink restores the
 * split) and safe: a wrong link loses nothing. Only the Android target implements
 * it; other targets report unsupported.
 */
expect class ContactsWriter() {
    /** True when this build can write contacts (Android + WRITE_CONTACTS declared). */
    fun isSupported(): Boolean

    /** True when [isSupported] and the user has granted WRITE_CONTACTS. */
    fun hasAccess(): Boolean

    /**
     * Link every device raw-contact that carries any of [phones] or [emails] into
     * one aggregated contact. Reversible (aggregation, not deletion). Returns the
     * number of raw contacts linked; 0 or 1 means there was nothing to merge.
     */
    suspend fun linkByIdentity(phones: List<String>, emails: List<String>): Int
}
