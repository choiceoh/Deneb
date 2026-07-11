// Package briefcase contains the deterministic, isolated runtime primitives
// used by Deneb-Briefcase evaluations.
//
// The package deliberately has no network or process-execution surface. A run
// receives a fresh filesystem root, an explicit clock, a deterministic event
// timeline, and a scripted device twin. Policy checks fail closed for unknown
// operations and unplanned device actions.
package briefcase
