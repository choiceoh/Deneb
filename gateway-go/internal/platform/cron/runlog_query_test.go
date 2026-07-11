package cron

import "testing"

func TestBuildRunLogPageSharesFilteringSortingAndPagination(t *testing.T) {
	entries := []RunLogEntry{
		{Ts: 20, JobID: "weekly", Status: "ok", DeliveryStatus: "sent", Summary: "later report"},
		{Ts: 10, JobID: "daily", Status: "error", DeliveryStatus: "failed", Error: "network timeout"},
		{Ts: 30, JobID: "daily", Status: "error", DeliveryStatus: "sent", Summary: "timeout recovered"},
	}

	page := buildRunLogPage(entries, RunLogReadOpts{Status: "error", Query: "TIMEOUT", Limit: 1})
	if page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].Ts != 30 {
		t.Fatalf("descending page = %#v", page)
	}
	if !page.HasMore || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatalf("pagination = %#v", page)
	}

	page = buildRunLogPage(entries, RunLogReadOpts{
		Status:         "error",
		DeliveryStatus: "failed",
		Query:          "daily",
		SortDir:        "asc",
		Offset:         -4,
	})
	if page.Offset != 0 || page.Total != 1 || page.Entries[0].Ts != 10 {
		t.Fatalf("filtered ascending page = %#v", page)
	}
}

func TestBuildRunLogPageClampsLimitAndEmptyOffset(t *testing.T) {
	entries := []RunLogEntry{{Ts: 1}, {Ts: 2}}
	page := buildRunLogPage(entries, RunLogReadOpts{Limit: 5001, Offset: 4})
	if page.Limit != 200 || page.Total != 2 || len(page.Entries) != 0 || page.HasMore || page.NextOffset != nil {
		t.Fatalf("empty page = %#v", page)
	}
}
