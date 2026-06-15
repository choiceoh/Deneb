// Package knowledge exposes the wiki knowledge base under a single
// agent-facing surface. Polaris (session-bound) and graphify (graph-traversal
// paradigm) intentionally stay separate.
//
// The agent sees three operations:
//
//	recall(query)        — semantic + lexical search, merged by score
//	read(ref)            — fetch one document by its layered reference
//	record(page, body)   — write a curated wiki entry
//
// Refs use a `<layer>:<id>` scheme; the router dispatches by prefix. The
// Router/Adapter layering is kept so a second backend can slot in later.
package knowledge
