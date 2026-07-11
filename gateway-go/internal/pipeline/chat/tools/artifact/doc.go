// Package artifact owns the agent's file and media artifact lifecycle: create
// charts and diagrams, inspect audio/video/documents, browse the file store,
// read spilled outputs, and deliver files through the active channel.
//
// The package has no dependency on its parent tools package. Registration and
// remaining root tools compose its exported tool constructors directly.
package artifact
