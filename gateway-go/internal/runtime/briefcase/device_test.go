package briefcase

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDeviceTwinOutcomesDeduplicatesRepeatedActions(t *testing.T) {
	start := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	twin, err := NewDeviceTwin(clock, []DevicePlan{
		{ActionID: "confirm", Kind: "notify", Payload: json.RawMessage(`{"body":"hi","title":"Deneb"}`), Status: DeviceConfirmed, Result: json.RawMessage(`{"receipt":"r1"}`)},
		{ActionID: "fail", Kind: "alarm", Payload: json.RawMessage(`{"hour":9}`), Status: DeviceFailed, Failure: "permission denied"},
		{ActionID: "unknown", Kind: "open_app", Payload: json.RawMessage(`{"app":"mail"}`), Status: DeviceUnconfirmed},
		{ActionID: "delay", Kind: "speak", Payload: json.RawMessage(`{"text":"briefing"}`), Status: DeviceDelayed, Delay: 5 * time.Minute, FinalStatus: DeviceConfirmed, Result: json.RawMessage(`{"spoken":true}`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	confirmed, err := twin.Perform(DeviceAction{ActionID: "confirm", Kind: "notify", Payload: json.RawMessage(`{"title":"Deneb","body":"hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Record.Status != DeviceConfirmed || confirmed.Duplicate {
		t.Fatalf("confirmed result = %#v", confirmed)
	}
	duplicate, err := twin.Perform(DeviceAction{ActionID: "confirm", Kind: "notify", Payload: json.RawMessage(`{"body":"hi","title":"Deneb"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Record.DuplicateAttempts != 1 {
		t.Fatalf("duplicate result = %#v", duplicate)
	}
	if _, err := twin.Perform(DeviceAction{ActionID: "confirm", Kind: "notify", Payload: json.RawMessage(`{"body":"different","title":"Deneb"}`)}); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("conflicting duplicate error = %v, want ErrActionConflict", err)
	}

	failed, err := twin.Perform(DeviceAction{ActionID: "fail", Kind: "alarm", Payload: json.RawMessage(`{"hour":9}`)})
	if err != nil {
		t.Fatal(err)
	}
	unconfirmed, err := twin.Perform(DeviceAction{ActionID: "unknown", Kind: "open_app", Payload: json.RawMessage(`{"app":"mail"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Record.Status != DeviceFailed || failed.Record.Failure == "" || unconfirmed.Record.Status != DeviceUnconfirmed {
		t.Fatalf("terminal outcomes: failed=%#v unconfirmed=%#v", failed, unconfirmed)
	}

	delayed, err := twin.Perform(DeviceAction{ActionID: "delay", Kind: "speak", Payload: json.RawMessage(`{"text":"briefing"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if delayed.Record.Status != DeviceDelayed || !delayed.Record.ReadyAt.Equal(start.Add(5*time.Minute)) || !delayed.Record.ResolvedAt.IsZero() {
		t.Fatalf("delayed record = %#v", delayed.Record)
	}
	if got := twin.SettleDue(); len(got) != 0 {
		t.Fatalf("settled early: %#v", got)
	}
	if err := clock.Advance(10 * time.Minute); err != nil {
		t.Fatal(err)
	}
	settled := twin.SettleDue()
	if len(settled) != 1 || settled[0].Status != DeviceConfirmed || !settled[0].ResolvedAt.Equal(start.Add(5*time.Minute)) {
		t.Fatalf("settled = %#v", settled)
	}
	if got := twin.SettleDue(); len(got) != 0 {
		t.Fatalf("settled twice: %#v", got)
	}

	ledger := twin.Ledger()
	var ids []string
	for _, record := range ledger {
		ids = append(ids, record.ActionID)
	}
	if want := []string{"confirm", "fail", "unknown", "delay"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ledger order = %v, want %v", ids, want)
	}
	if len(ledger) != 4 {
		t.Fatalf("duplicate created a second ledger entry: %d", len(ledger))
	}
}

func TestDeviceTwinFailsClosedForUnplannedAndInvalidPlans(t *testing.T) {
	clock := NewFrozenClock(time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC))
	twin, err := NewDeviceTwin(clock, []DevicePlan{{ActionID: "known", Kind: "notify", Payload: json.RawMessage(`null`), Status: DeviceConfirmed}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := twin.Perform(DeviceAction{ActionID: "other", Kind: "notify"}); !errors.Is(err, ErrUnplannedAction) {
		t.Fatalf("unplanned error = %v", err)
	}
	if len(twin.Ledger()) != 0 {
		t.Fatal("unplanned action entered ledger")
	}

	badPlans := []DevicePlan{
		{ActionID: "x", Kind: "notify", Status: "surprise"},
		{ActionID: "x", Kind: "notify", Status: DeviceDelayed, Delay: 0, FinalStatus: DeviceConfirmed},
		{ActionID: "x", Kind: "notify", Status: DeviceDelayed, Delay: time.Second, FinalStatus: DeviceDelayed},
		{ActionID: "x", Kind: "notify", Status: DeviceConfirmed, Delay: time.Second},
	}
	for _, plan := range badPlans {
		if _, err := NewDeviceTwin(clock, []DevicePlan{plan}); !errors.Is(err, ErrInvalidDevicePlan) {
			t.Errorf("plan %#v error = %v, want ErrInvalidDevicePlan", plan, err)
		}
	}
}

func TestTimelineWithDeviceTwinStaysDeterministic(t *testing.T) {
	start := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	twin, err := NewDeviceTwin(clock, []DevicePlan{{
		ActionID: "notify-1", Kind: "notify", Payload: json.RawMessage(`{"text":"ready"}`),
		Status: DeviceDelayed, Delay: 2 * time.Minute, FinalStatus: DeviceUnconfirmed,
	}})
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := NewTimeline(clock, []TimelineEvent{
		{ID: "request", Kind: "device.request", At: start, Payload: json.RawMessage(`{"actionId":"notify-1"}`)},
		{ID: "poll", Kind: "device.poll", At: start.Add(3 * time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var settled []DeviceActionRecord
	if err := timeline.ReplayAll(t.Context(), func(_ context.Context, event TimelineEvent) error {
		switch event.Kind {
		case "device.request":
			_, performErr := twin.Perform(DeviceAction{
				ActionID: "notify-1", Kind: "notify", Payload: json.RawMessage(`{"text":"ready"}`),
			})
			return performErr
		case "device.poll":
			settled = twin.SettleDue()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(settled) != 1 || settled[0].Status != DeviceUnconfirmed ||
		!settled[0].ResolvedAt.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("settled integration result = %#v", settled)
	}
}

func TestDevicePlansDigestNormalizesOrderAndDetectsChange(t *testing.T) {
	left := []DevicePlan{
		{ActionID: "b", Kind: "notify", Payload: json.RawMessage(`{"x":1,"y":2}`), Status: DeviceConfirmed},
		{ActionID: "a", Kind: "alarm", Payload: json.RawMessage(`{"at":"09:00"}`), Status: DeviceFailed, Failure: "denied"},
	}
	right := []DevicePlan{
		{ActionID: "a", Kind: "alarm", Payload: json.RawMessage(`{ "at" : "09:00" }`), Status: DeviceFailed, Failure: "denied"},
		{ActionID: "b", Kind: "notify", Payload: json.RawMessage(`{"y":2,"x":1}`), Status: DeviceConfirmed},
	}
	leftDigest, err := DevicePlansDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := DevicePlansDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest || len(leftDigest) != 64 {
		t.Fatalf("canonical digests = %q / %q", leftDigest, rightDigest)
	}
	right[0].Failure = "different"
	changed, err := DevicePlansDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if changed == leftDigest {
		t.Fatal("device-plan digest ignored a semantic change")
	}
}

func TestDecodeDevicePlanSourceIsStrict(t *testing.T) {
	plans, err := DecodeDevicePlanSource([]byte(`{"plans":[{"actionId":"n1","kind":"notify","payload":{"text":"hi"},"status":"confirmed","delaySeconds":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].ActionID != "n1" || plans[0].Delay != 2*time.Second {
		t.Fatalf("decoded plans = %+v", plans)
	}
	for _, source := range []string{
		`{"plans":[],"plans":[]}`,
		`{"plans":[{"actionId":"n1","kind":"notify","status":"confirmed","unknown":true}]}`,
	} {
		if _, err := DecodeDevicePlanSource([]byte(source)); !errors.Is(err, ErrInvalidDevicePlan) {
			t.Fatalf("invalid device source error = %v", err)
		}
	}
}
