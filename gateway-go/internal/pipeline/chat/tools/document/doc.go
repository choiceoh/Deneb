// Package document extracts readable text from documents and image
// attachments. It owns format detection, Office/PDF parsers, OCR fallback, and
// the bounded attachment presentation policies shared by chat tools.
//
// The package deliberately has no dependency on its parent tools package;
// callers compose the narrow extraction functions they need.
package document
