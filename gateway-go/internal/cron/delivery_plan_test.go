package cron

import "testing"

func TestResolveCronDeliveryPlanFromStore_WithDelivery(t *testing.T) {
	job := StoreJob{
		Delivery: &JobDeliveryConfig{
			Channel:   "telegram",
			To:        "12345",
			AccountID: "acct-1",
		},
	}
	plan := ResolveCronDeliveryPlanFromStore(job)
	if plan.Mode != DeliveryModeAnnounce {
		t.Errorf("expected announce, got %s", plan.Mode)
	}
	if plan.Channel != "telegram" {
		t.Errorf("expected telegram, got %s", plan.Channel)
	}
	if plan.To != "12345" {
		t.Errorf("expected 12345, got %s", plan.To)
	}
	if plan.Source != "delivery" {
		t.Errorf("expected source=delivery, got %s", plan.Source)
	}
	if !plan.Requested {
		t.Error("expected requested=true")
	}
}

func TestResolveCronDeliveryPlanFromStore_NoDelivery(t *testing.T) {
	job := StoreJob{}
	plan := ResolveCronDeliveryPlanFromStore(job)
	if plan.Mode != DeliveryModeNone {
		t.Errorf("expected none, got %s", plan.Mode)
	}
	if plan.Requested {
		t.Error("expected requested=false")
	}
}

func TestResolveFailureDestination_GlobalOnly(t *testing.T) {
	global := &CronFailureDestinationConfig{
		Channel: "telegram",
		To:      "99",
		Mode:    "announce",
	}
	result := ResolveFailureDestination(nil, global)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Mode != "announce" {
		t.Errorf("expected announce, got %s", result.Mode)
	}
	if result.Channel != "telegram" {
		t.Errorf("expected telegram, got %s", result.Channel)
	}
	if result.To != "99" {
		t.Errorf("expected 99, got %s", result.To)
	}
}

func TestResolveFailureDestination_NilBoth(t *testing.T) {
	result := ResolveFailureDestination(nil, nil)
	if result != nil {
		t.Fatal("expected nil for no config")
	}
}

func TestResolveFailureDestination_WebhookRequiresTo(t *testing.T) {
	global := &CronFailureDestinationConfig{
		Mode: "webhook",
		// No To URL.
	}
	result := ResolveFailureDestination(nil, global)
	if result != nil {
		t.Fatal("expected nil when webhook has no URL")
	}
}

func TestResolveFailureDestination_JobOverridesGlobal(t *testing.T) {
	global := &CronFailureDestinationConfig{
		Channel: "slack",
		To:      "old",
	}
	delivery := &CronDeliveryFull{
		Mode: DeliveryModeAnnounce,
		FailureDestination: &CronFailureDestination{
			Channel: "telegram",
			To:      "new",
		},
	}
	result := ResolveFailureDestination(delivery, global)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Channel != "telegram" {
		t.Errorf("expected telegram, got %s", result.Channel)
	}
	if result.To != "new" {
		t.Errorf("expected new, got %s", result.To)
	}
}

func TestShouldSendFailureAlert(t *testing.T) {
	alert := &CronFailureAlert{After: 3, CooldownMs: 60000}
	now := int64(1000000)

	// Not enough consecutive errors.
	state := JobState{ConsecutiveErrors: 2}
	if ShouldSendFailureAlert(state, alert, "error", now) {
		t.Error("expected false when under threshold")
	}

	// Enough errors.
	state.ConsecutiveErrors = 3
	if !ShouldSendFailureAlert(state, alert, "error", now) {
		t.Error("expected true when at threshold")
	}

	// Cooldown active.
	state.LastFailureAlertAtMs = now - 30000 // 30s ago
	if ShouldSendFailureAlert(state, alert, "error", now) {
		t.Error("expected false during cooldown")
	}

	// Cooldown expired.
	state.LastFailureAlertAtMs = now - 70000 // 70s ago
	if !ShouldSendFailureAlert(state, alert, "error", now) {
		t.Error("expected true after cooldown")
	}

	// Not an error.
	if ShouldSendFailureAlert(state, alert, "ok", now) {
		t.Error("expected false for ok status")
	}

	// Nil alert.
	if ShouldSendFailureAlert(state, nil, "error", now) {
		t.Error("expected false for nil alert")
	}
}
