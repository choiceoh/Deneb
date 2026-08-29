package mailarchive

import "testing"

// The sender history the analysis actually receives is min(maxSender,
// maxFetch-maxThread), not maxSender. At maxFetch 18 a 10+10 configuration
// delivered 8 sender messages while every constant said 10 — the trim happens
// inside prioritizedArchiveUIDGroups and is invisible from the config.
func TestDefaultFetchCapCoversThreadPlusSender(t *testing.T) {
	if defaultMaxFetch < defaultMaxThread+defaultMaxSender {
		t.Fatalf("maxFetch %d < thread %d + sender %d: sender history is silently trimmed",
			defaultMaxFetch, defaultMaxThread, defaultMaxSender)
	}
}

func TestDefaultsDeliverTheFullSenderHistory(t *testing.T) {
	thread := make([]string, defaultMaxThread)
	for i := range thread {
		thread[i] = "t" + string(rune('a'+i))
	}
	sender := make([]string, defaultMaxSender)
	for i := range sender {
		sender[i] = "s" + string(rune('a'+i))
	}
	groups := prioritizedArchiveUIDGroups(thread, sender,
		defaultMaxThread, defaultMaxSender, defaultMaxFetch)
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	if total != defaultMaxThread+defaultMaxSender {
		t.Fatalf("got %d messages, want %d — the fetch cap is trimming",
			total, defaultMaxThread+defaultMaxSender)
	}
}

// The window is what makes the count meaningful: 10 messages inside 30 days is
// a different instrument from 10 inside 90.
func TestSenderWindowIsNinetyDays(t *testing.T) {
	const day = 24 * 60 * 60
	if got := int(defaultSenderWindow.Seconds()) / day; got != 90 {
		t.Fatalf("sender window = %d days, want 90", got)
	}
}
