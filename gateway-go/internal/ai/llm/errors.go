package llm

import (
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

// ClassifyError runs the shared LLM error classifier, lifting the HTTP status
// and provider body out of this package's APIError transport type.
func ClassifyError(err error) llmerr.Classified {
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
