package genesis

import (
	"crypto/sha256"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"
)

// Counterfactual holdout for the L4 coding lane.
//
// The lane's output is easy to count and its VALUE is not: 38 landed changes is
// not 38 changes worth making, and nothing measures what would have happened
// without them. Asked "does RSI pay for itself" (2026-08-01) the honest answer
// was that the system cannot tell, because every candidate is dispatched — there
// is no control group.
//
// So withhold a deterministic slice. A held-out candidate is mined, reviewed and
// ledgered exactly like any other; it simply is not handed to a coding session
// for a while. Comparing how often held-out findings resolve on their own
// against the dispatched ones measures the lane's MARGINAL value, using data the
// system already produces.
//
// Three properties make this safe to leave on:
//
//   - Deterministic. Membership is a hash of the candidate id, so a candidate
//     never flips sides between ticks and the analysis can recompute membership
//     later without any extra state to keep (or to rot).
//   - Time-boxed. The hold expires; after the window the candidate dispatches
//     normally. A control group is worth a delay, not an abandoned defect.
//   - Off-switch. DENEB_RSI_HOLDOUT_PERCENT=0 disables it outright.
const (
	selfCorrectionHoldoutDefaultPercent = 10
	selfCorrectionHoldoutWindow         = 14 * 24 * time.Hour
)

// selfCorrectionHoldoutPercent reads the configured share, clamped to [0,50].
// Above half the lane would be mostly idle, which trades away the thing being
// measured; the clamp makes a fat-fingered env harmless.
func selfCorrectionHoldoutPercent() int {
	raw := strings.TrimSpace(os.Getenv("DENEB_RSI_HOLDOUT_PERCENT"))
	if raw == "" {
		return selfCorrectionHoldoutDefaultPercent
	}
	pct, err := strconv.Atoi(raw)
	if err != nil || pct < 0 {
		return selfCorrectionHoldoutDefaultPercent
	}
	if pct > 50 {
		return 50
	}
	return pct
}

// selfCorrectionHoldoutBucket maps a candidate id to a stable [0,100) bucket.
// sha256 rather than a fast hash: the ids are short and sequential-ish, and a
// weak mixer would correlate the bucket with mining order — which would make
// the holdout a sample of WHEN a finding was mined, not a random slice.
func selfCorrectionHoldoutBucket(id string) int {
	sum := sha256.Sum256([]byte(strings.TrimSpace(id)))
	return int(binary.BigEndian.Uint32(sum[:4]) % 100)
}

// selfCorrectionHeldOut reports whether a candidate is currently withheld from
// dispatch as the control group. Expired holds return false so the candidate
// rejoins the normal queue.
func selfCorrectionHeldOut(record SelfCorrectionCandidateRecord, now time.Time) bool {
	pct := selfCorrectionHoldoutPercent()
	if pct <= 0 || strings.TrimSpace(record.ID) == "" {
		return false
	}
	if selfCorrectionHoldoutBucket(record.ID) >= pct {
		return false
	}
	created := record.CreatedAt
	if created <= 0 {
		return false // no clock to expire against; do not withhold indefinitely
	}
	return now.Before(time.UnixMilli(created).Add(selfCorrectionHoldoutWindow))
}

// SelfCorrectionHeldOut is the exported form for status surfaces and offline
// analysis. Membership is recomputable from the id alone, so a later comparison
// needs nothing persisted at hold time.
func SelfCorrectionHeldOut(record SelfCorrectionCandidateRecord, now time.Time) bool {
	return selfCorrectionHeldOut(record, now)
}
