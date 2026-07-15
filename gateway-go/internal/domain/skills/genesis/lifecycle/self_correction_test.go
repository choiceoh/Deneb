package lifecycle

import "testing"

func TestReviewAndDeliveryAxesStayIndependentUntilWatchPasses(t *testing.T) {
	if !CanReviewTransition(ReviewProposed, ReviewAccepted) || CanReviewTransition(ReviewApplied, ReviewAccepted) {
		t.Fatal("review transition contract changed")
	}
	if CanReviewTransition("", ReviewAccepted) || CanDispatch("", "") {
		t.Fatal("missing review state must fail closed")
	}
	if got := ReviewAfterDelivery(ReviewAccepted, DeliveryDeployed); got != ReviewAccepted {
		t.Fatalf("deployed review = %q, want accepted", got)
	}
	if got := ReviewAfterDelivery(ReviewAccepted, DeliveryWatchPassed); got != ReviewApplied {
		t.Fatalf("watched review = %q, want applied", got)
	}
}

func TestDeliveryTransitionsAllowReconciliationAndFreshRetryOnly(t *testing.T) {
	tests := []struct {
		from DeliveryPhase
		to   DeliveryPhase
		want bool
	}{
		{"", DeliveryStarted, true},
		{DeliveryStarted, DeliveryMerged, true},
		{DeliveryFailed, DeliveryMerged, true},
		{DeliveryFailed, DeliveryStarted, true},
		{DeliveryMerged, DeliveryStarted, false},
		{DeliveryWatchPassed, DeliveryStarted, false},
	}
	for _, test := range tests {
		if got := CanDeliveryTransition(test.from, test.to); got != test.want {
			t.Fatalf("CanDeliveryTransition(%q, %q) = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestClassifyDispatchResultUsesFactsAndFailsClosed(t *testing.T) {
	negative, zero, one := -1, 0, 1
	tests := []struct {
		name    string
		facts   DispatchFacts
		want    DeliveryPhase
		wantErr bool
	}{
		{name: "merged", facts: DispatchFacts{PRState: "MERGED"}, want: DeliveryMerged},
		{name: "open", facts: DispatchFacts{PRState: "open"}, want: DeliveryPROpened},
		{name: "declined", facts: DispatchFacts{Ahead: &zero}, want: DeliveryDeclined},
		{name: "unlanded work", facts: DispatchFacts{Ahead: &one}, want: DeliveryFailed},
		{name: "process failed", facts: DispatchFacts{ReturnCode: 1}, want: DeliveryFailed},
		{name: "unknown clean", facts: DispatchFacts{}, wantErr: true},
		{name: "invalid PR state", facts: DispatchFacts{Ahead: &zero, PRState: "mystery"}, wantErr: true},
		{name: "negative ahead", facts: DispatchFacts{Ahead: &negative}, wantErr: true},
		{name: "invalid return code", facts: DispatchFacts{ReturnCode: 256}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClassifyDispatchResult(test.facts)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("ClassifyDispatchResult(%+v) = %q, %v; want %q err=%v", test.facts, got, err, test.want, test.wantErr)
			}
		})
	}
}
