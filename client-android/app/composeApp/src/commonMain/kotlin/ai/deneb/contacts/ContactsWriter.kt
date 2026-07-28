package ai.deneb.contacts

/**
 * Outcome of [ContactsWriter.resetMergeLinks]: how many forced merges were undone,
 * and where the pre-reset pairs were saved so the undo is itself reversible.
 * [backupPath] is empty when nothing was written (unsupported target, or nothing
 * to undo).
 */
data class ContactsResetResult(
    val undone: Int = 0,
    val backupPath: String = "",
)

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
     * Link the device raw-contacts belonging to ONE person into one aggregated
     * contact. A raw contact qualifies only when it carries one of [phones]/[emails]
     * AND its display name is one of [names] — the merge group the gateway named.
     *
     * The name gate is load-bearing, not defensive polish. Matching on identifiers
     * alone linked everyone who shared a company line, then cascaded through their
     * numbers (2026-07-28: 2,819 entries collapsed to 1,270, people wearing each
     * other's names). Pass the group's identifiers AND its names, always.
     *
     * Reversible (aggregation, not deletion). Returns the number of raw contacts
     * linked; 0 or 1 means there was nothing to merge.
     */
    suspend fun linkByIdentity(phones: List<String>, emails: List<String>, names: List<String>): Int

    /** How many forced merges (KEEP_TOGETHER pairs) currently exist on the device. */
    suspend fun countMergeLinks(): Int

    /**
     * Undo every forced merge: each KEEP_TOGETHER pair goes back to TYPE_AUTOMATIC,
     * returning the device to Android's own automatic aggregation — the state before
     * any apply ran. KEEP_SEPARATE rows are deliberately left alone: those say "never
     * merge these", so clearing them would create merges instead of undoing them.
     *
     * Contact data is never touched — only the aggregation hints. The pairs are saved
     * to a backup file first, because once a pair goes back to AUTOMATIC the provider
     * forgets it and there is no way to recompute which pairs were ours.
     */
    suspend fun resetMergeLinks(): ContactsResetResult
}
