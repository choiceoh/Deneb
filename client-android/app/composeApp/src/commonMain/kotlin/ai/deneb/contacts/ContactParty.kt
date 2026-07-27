package ai.deneb.contacts

/** One address-book card from the dedup mirror, used to scope apply-time linking. */
data class ContactParty(
    val name: String,
    val phones: List<String> = emptyList(),
    val emails: List<String> = emptyList(),
)
