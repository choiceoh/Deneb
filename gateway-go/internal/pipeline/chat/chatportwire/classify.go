package chatportwire

import (
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

// Classify runs llmerr.Classify against an error, lifting the HTTP status out of
// any wrapped *httpretry.APIError so the classifier's status pipeline is engaged.
func Classify(err error) llmerr.Classified {
	var apiErr *httpretry.APIError
	status := 0
	var body []byte
	if errors.As(err, &apiErr) {
		status = apiErr.StatusCode
		if apiErr.Message != "" {
			body = []byte(apiErr.Message)
		}
	}
	return llmerr.Classify(err, status, body)
}

// IsContextOverflow reports whether an error indicates a context window overflow.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	return Classify(err).Reason == llmerr.ReasonContextOverflow
}
