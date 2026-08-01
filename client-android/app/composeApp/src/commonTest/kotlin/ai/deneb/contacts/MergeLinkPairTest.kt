package ai.deneb.contacts

import kotlin.test.Test
import kotlin.test.assertEquals

class MergeLinkPairTest {
    @Test
    fun mergeLinksAddedIsAfterMinusBefore() {
        val before = setOf(MergeLinkPair.of(1, 2)!!, MergeLinkPair.of(3, 4)!!)
        val after = setOf(
            MergeLinkPair.of(1, 2)!!,
            MergeLinkPair.of(3, 4)!!,
            MergeLinkPair.of(5, 6)!!,
        )
        assertEquals(setOf(MergeLinkPair.of(5, 6)!!), mergeLinksAdded(before, after))
    }

    @Test
    fun mergeLinksAddedEmptyWhenNothingNew() {
        val snap = setOf(MergeLinkPair.of(10, 20)!!)
        assertEquals(emptySet(), mergeLinksAdded(snap, snap))
    }
}
