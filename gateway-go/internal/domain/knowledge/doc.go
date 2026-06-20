// Package knowledge exposes the curated wiki and the on-box file store under a
// single agent-facing surface. Polaris (session-bound) and graphify
// (graph-traversal paradigm) intentionally stay separate.
//
// The agent sees three operations:
//
//	recall(query)        — semantic + lexical search across backends, merged by score
//	read(ref)            — fetch one document by its layered reference
//	record(page, body)   — write a curated wiki entry (wiki is the only writable layer)
//
// Backends (read adapters):
//
//	w:<path>  — curated wiki page (writable)
//	f:<path>  — on-box file store, hybrid (BM25 + dense-vector) semantic search
//
// Refs use a `<layer>:<id>` scheme; the router dispatches by prefix. The
// Router/Adapter layering lets a backend slot in (or degrade out) without
// touching call sites — a nil adapter is simply dropped.
package knowledge
