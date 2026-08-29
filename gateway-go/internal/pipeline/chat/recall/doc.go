// Package recall owns multi-source memory evidence gathering and frozen
// per-session recall snapshots. BuildSnapshot is the parent chat package's
// high-level port; lower-level cache and citation helpers stay package-local so
// evidence policy changes do not leak into turn orchestration.
package recall
