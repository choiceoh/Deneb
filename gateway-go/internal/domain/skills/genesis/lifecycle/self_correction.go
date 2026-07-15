package lifecycle

import (
	"errors"
	"strings"
)

// ReviewState is the operator or deterministic review decision. Delivery is a
// separate axis: accepted means approved to try, while applied means verified.
type ReviewState string

const (
	ReviewProposed   ReviewState = "proposed"
	ReviewAccepted   ReviewState = "accepted"
	ReviewRejected   ReviewState = "rejected"
	ReviewSuperseded ReviewState = "superseded"
	ReviewApplied    ReviewState = "applied"
)

// DeliveryPhase is the authoritative L4 execution lifecycle.
type DeliveryPhase string

const (
	DeliveryStarted     DeliveryPhase = "started"
	DeliveryPROpened    DeliveryPhase = "pr_opened"
	DeliveryMerged      DeliveryPhase = "merged"
	DeliveryDeployed    DeliveryPhase = "deployed"
	DeliveryWatchPassed DeliveryPhase = "watch_passed"
	DeliveryDeclined    DeliveryPhase = "declined"
	DeliveryFailed      DeliveryPhase = "failed"
	DeliveryRolledBack  DeliveryPhase = "rolled_back"
)

// DeliveryClass is the small read-side vocabulary shared by dispatch and RSI
// status. It prevents each consumer from rebuilding a phase switch.
type DeliveryClass string

const (
	DeliveryQueued    DeliveryClass = "queued"
	DeliveryInFlight  DeliveryClass = "in_flight"
	DeliveryVerified  DeliveryClass = "verified"
	DeliverySafeNoop  DeliveryClass = "safe_noop"
	DeliveryRetryable DeliveryClass = "retryable"
)

// DispatchFacts are deterministic session facts. The model's prose is never
// consulted when deciding the authoritative delivery phase.
type DispatchFacts struct {
	ReturnCode int
	Ahead      *int
	PRState    string
}

var (
	ErrInsufficientDispatchFacts = errors.New("insufficient dispatch facts")
	ErrInvalidDispatchFacts      = errors.New("invalid dispatch facts")
)

// NormalizeReview accepts the compatibility spellings used by RPC callers.
func NormalizeReview(value string) ReviewState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "propose", "proposed", "pending", "open":
		return ReviewProposed
	case "accept", "accepted":
		return ReviewAccepted
	case "reject", "rejected":
		return ReviewRejected
	case "supersede", "superseded":
		return ReviewSuperseded
	case "apply", "applied":
		return ReviewApplied
	default:
		return ""
	}
}

// CanReviewTransition validates the review axis independently of delivery.
func CanReviewTransition(from, to ReviewState) bool {
	from = NormalizeReview(string(from))
	to = NormalizeReview(string(to))
	switch from {
	case ReviewProposed:
		return to == ReviewAccepted || to == ReviewRejected || to == ReviewSuperseded || to == ReviewApplied
	case ReviewAccepted:
		return to == ReviewRejected || to == ReviewSuperseded || to == ReviewApplied
	default:
		return false
	}
}

// NormalizeDelivery returns an empty value for unknown phases.
func NormalizeDelivery(value string) DeliveryPhase {
	switch DeliveryPhase(strings.ToLower(strings.TrimSpace(value))) {
	case DeliveryStarted, DeliveryPROpened, DeliveryMerged, DeliveryDeployed,
		DeliveryWatchPassed, DeliveryDeclined, DeliveryFailed, DeliveryRolledBack:
		return DeliveryPhase(strings.ToLower(strings.TrimSpace(value)))
	default:
		return ""
	}
}

// CanDeliveryTransition validates one attempt and the permitted retry edges.
func CanDeliveryTransition(from, to DeliveryPhase) bool {
	from = NormalizeDelivery(string(from))
	to = NormalizeDelivery(string(to))
	if from == "" {
		return to == DeliveryStarted
	}
	if to == DeliveryStarted {
		return from == DeliveryFailed || from == DeliveryRolledBack
	}
	switch from {
	case DeliveryStarted:
		return to == DeliveryPROpened || to == DeliveryMerged || to == DeliveryDeclined || to == DeliveryFailed
	case DeliveryPROpened:
		return to == DeliveryMerged || to == DeliveryDeclined || to == DeliveryFailed
	case DeliveryFailed:
		return to == DeliveryPROpened || to == DeliveryMerged
	case DeliveryMerged:
		return to == DeliveryDeployed || to == DeliveryFailed
	case DeliveryDeployed:
		return to == DeliveryWatchPassed || to == DeliveryRolledBack
	default:
		return false
	}
}

// ClassifyDelivery maps detailed phases to the shared read-side lifecycle.
func ClassifyDelivery(phase DeliveryPhase) DeliveryClass {
	switch NormalizeDelivery(string(phase)) {
	case DeliveryStarted, DeliveryPROpened, DeliveryMerged, DeliveryDeployed:
		return DeliveryInFlight
	case DeliveryWatchPassed:
		return DeliveryVerified
	case DeliveryDeclined:
		return DeliverySafeNoop
	case DeliveryFailed, DeliveryRolledBack:
		return DeliveryRetryable
	default:
		return DeliveryQueued
	}
}

// ReviewAfterDelivery derives applied only from a verified delivery. Other
// delivery phases never overwrite the independent review decision.
func ReviewAfterDelivery(review ReviewState, phase DeliveryPhase) ReviewState {
	if NormalizeDelivery(string(phase)) == DeliveryWatchPassed {
		return ReviewApplied
	}
	return NormalizeReview(string(review))
}

// CanDispatch reports whether a reviewed candidate is eligible for a new
// attempt before source/surface policy is applied.
func CanDispatch(review ReviewState, phase DeliveryPhase) bool {
	review = NormalizeReview(string(review))
	if review != ReviewProposed && review != ReviewAccepted {
		return false
	}
	class := ClassifyDelivery(phase)
	return class == DeliveryQueued || class == DeliveryRetryable
}

// ClassifyDispatchResult deterministically closes one agent session from git
// and GitHub facts. Unknown clean results fail closed instead of inventing a
// decline.
func ClassifyDispatchResult(facts DispatchFacts) (DeliveryPhase, error) {
	prState := strings.ToUpper(strings.TrimSpace(facts.PRState))
	if facts.ReturnCode < 0 || facts.ReturnCode > 255 || facts.Ahead != nil && *facts.Ahead < 0 {
		return "", ErrInvalidDispatchFacts
	}
	switch prState {
	case "MERGED":
		return DeliveryMerged, nil
	case "OPEN":
		return DeliveryPROpened, nil
	case "", "CLOSED":
	default:
		return "", ErrInvalidDispatchFacts
	}
	if facts.ReturnCode != 0 {
		return DeliveryFailed, nil
	}
	if facts.Ahead == nil {
		return "", ErrInsufficientDispatchFacts
	}
	if *facts.Ahead == 0 {
		return DeliveryDeclined, nil
	}
	return DeliveryFailed, nil
}
