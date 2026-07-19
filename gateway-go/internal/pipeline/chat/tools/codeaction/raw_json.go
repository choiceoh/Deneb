package codeaction

import "encoding/json"

// rawJSON is an unexported alias for verbatim JSON at the package boundary.
type rawJSON = json.RawMessage
