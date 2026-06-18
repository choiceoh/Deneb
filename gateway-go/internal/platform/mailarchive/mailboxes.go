package mailarchive

import "strings"

const (
	MailboxInbox       = "INBOX"
	MailboxArchive     = "Archive"
	MailboxAllMail     = "All Mail"
	MailboxLegacyGmail = "Gmail"
)

// DefaultMailboxes is the current production-safe archive view: live LMTP
// inbox plus the historical backfill mailbox. Operators can switch the
// backfill to Archive with DENEB_ARCHIVE_IMAP_MAILBOXES=INBOX,Archive.
func DefaultMailboxes() []string {
	return []string{MailboxInbox, MailboxLegacyGmail}
}

// ParseMailboxList parses DENEB_ARCHIVE_IMAP_MAILBOXES-style comma lists.
func ParseMailboxList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return cleanMailboxes(strings.Split(raw, ","))
}

// SelectMailboxes maps user/agent-facing mailbox selectors to physical archive
// candidates. The neutral "archive"/"backfill" selector lets prompts avoid the
// legacy Gmail name while still working before and after the physical mailbox
// rename.
func SelectMailboxes(selector string, configured []string) []string {
	selector = strings.TrimSpace(selector)
	switch normalizeMailboxSelector(selector) {
	case "", "all", "*":
		if clean := cleanMailboxes(configured); len(clean) > 0 {
			return clean
		}
		return DefaultMailboxes()
	case "inbox":
		return []string{MailboxInbox}
	case "archive", "backfill", "allmail", "all-mail", "general", "mail":
		if backfill := configuredBackfillMailboxes(configured); len(backfill) > 0 {
			return backfill
		}
		return []string{MailboxArchive, MailboxAllMail, MailboxLegacyGmail}
	case "legacy_gmail", "legacy-gmail", "gmail":
		return []string{MailboxLegacyGmail}
	default:
		return []string{selector}
	}
}

func configuredBackfillMailboxes(configured []string) []string {
	var out []string
	for _, mailbox := range cleanMailboxes(configured) {
		if strings.EqualFold(mailbox, MailboxInbox) {
			continue
		}
		out = append(out, mailbox)
	}
	return out
}

func cleanMailboxes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, mailbox := range in {
		mailbox = strings.TrimSpace(mailbox)
		if mailbox == "" {
			continue
		}
		key := strings.ToLower(mailbox)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mailbox)
	}
	return out
}

func lookupMailboxCandidates(mailbox string) []string {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return nil
	}
	out := []string{mailbox}
	switch normalizeMailboxSelector(mailbox) {
	case "gmail", "legacy_gmail", "legacy-gmail":
		out = append(out, MailboxArchive, MailboxAllMail)
	case "archive", "allmail", "all-mail", "backfill":
		out = append(out, MailboxLegacyGmail)
	}
	return cleanMailboxes(out)
}

func normalizeMailboxSelector(selector string) string {
	selector = strings.ToLower(strings.TrimSpace(selector))
	selector = strings.ReplaceAll(selector, " ", "")
	selector = strings.ReplaceAll(selector, "_", "-")
	return selector
}
