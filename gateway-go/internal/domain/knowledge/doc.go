// Package knowledge unifies cross-session persistent memory backends (wiki +
// hindsight) under a single agent-facing surface. Polaris (session-bound) and
// graphify (graph-traversal paradigm) intentionally stay separate.
//
// The agent sees three operations:
//
//	recall(query)        — federated search across all read backends, merged
//	read(ref)            — fetch one document by its layered reference
//	record(page, body)   — write a curated entry (wiki only; hindsight is
//	                       background auto-retained by hindsight_recorder)
//
// Refs use a `<layer>:<id>` scheme so the agent never picks a backend
// explicitly — the router dispatches by prefix.
package knowledge
