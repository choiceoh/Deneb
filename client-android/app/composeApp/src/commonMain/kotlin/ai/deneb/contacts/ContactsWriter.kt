package ai.deneb.contacts

/** One forced-merge pair (raw-contact ids), normalized so set ops are stable. */
data class MergeLinkPair(val a: Long, val b: Long) {
    init {
        require(a < b) { "MergeLinkPair must be normalized (a < b)" }
    }

    companion object {
        fun of(id1: Long, id2: Long): MergeLinkPair? {
            if (id1 == id2) return null
            return if (id1 < id2) MergeLinkPair(id1, id2) else MergeLinkPair(id2, id1)
        }
    }
}

/** Pairs added during a dedup apply = after snapshot minus the before snapshot. */
internal fun mergeLinksAdded(
    before: Set<MergeLinkPair>,
    after: Set<MergeLinkPair>,
): Set<MergeLinkPair> = after - before

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

    /**
     * Every forced-merge pair currently on the device. Used to diff before/after apply
     * so undo only touches links this feature created.
     */
    suspend fun snapshotMergeLinks(): Set<MergeLinkPair>

    /**
     * Record merge pairs added by a dedup apply (after − before snapshot). Repeated
     * applies accumulate; undo clears exactly this tracked set.
     */
    suspend fun trackDedupMergeLinks(pairs: Set<MergeLinkPair>)

    /** How many dedup-applied merge pairs are pending undo (not all device merges). */
    suspend fun countMergeLinks(): Int

    /**
     * Undo dedup-applied merges only: each tracked KEEP_TOGETHER pair goes back to
     * TYPE_AUTOMATIC. Pre-existing manual merges and KEEP_SEPARATE rows are left
     * alone. Contact data is never touched — only the aggregation hints. Tracked
     * pairs are backed up first because the provider forgets them once AUTOMATIC.
     */
    suspend fun resetMergeLinks(): ContactsResetResult
}
