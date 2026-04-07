package cron

import (
	"testing"
	"time"
)

func TestValidateScheduleTimestamp_NonAtSchedule(t *testing.T) {
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "cron", Expr: "0 * * * *"}, time.Now().UnixMilli())
	if !result.OK {
		t.Fatalf("expected OK for non-at schedule, got: %s", result.Message)
	}
}

func TestValidateScheduleTimestamp_ValidFuture(t *testing.T) {
	now := time.Now().UnixMilli()
	future := time.UnixMilli(now + 3600*1000).UTC().Format(time.RFC3339)
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "at", At: future}, now)
	if !result.OK {
		t.Fatalf("expected OK for future timestamp, got: %s", result.Message)
	}
}

func TestValidateScheduleTimestamp_PastRejected(t *testing.T) {
	now := time.Now().UnixMilli()
	past := time.UnixMilli(now - 5*60*1000).UTC().Format(time.RFC3339)
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "at", At: past}, now)
	if result.OK {
		t.Fatal("expected rejection for past timestamp")
	}
	if result.Message == "" {
		t.Fatal("expected error message")
	}
}

func TestValidateScheduleTimestamp_TooFarFuture(t *testing.T) {
	now := time.Now().UnixMilli()
	farFuture := time.UnixMilli(now + 11*365*24*3600*1000).UTC().Format(time.RFC3339)
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "at", At: farFuture}, now)
	if result.OK {
		t.Fatal("expected rejection for far-future timestamp")
	}
}

func TestValidateScheduleTimestamp_InvalidFormat(t *testing.T) {
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "at", At: "not-a-date"}, time.Now().UnixMilli())
	if result.OK {
		t.Fatal("expected rejection for invalid format")
	}
}

func TestValidateScheduleTimestamp_GracePeriod(t *testing.T) {
	now := time.Now().UnixMilli()
	// 30 seconds ago (within 1 minute grace).
	recent := time.UnixMilli(now - 30*1000).UTC().Format(time.RFC3339)
	result := ValidateScheduleTimestamp(StoreSchedule{Kind: "at", At: recent}, now)
	if !result.OK {
		t.Fatalf("expected OK within grace period, got: %s", result.Message)
	}
}
