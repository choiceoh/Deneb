// model_compat_internal.go — unexported shims used by autoreply root files.
// These delegate to the model subpackage so internal callers keep working.
// TODO: Remove after internal callers are updated to use model.ScoreFuzzyMatch
// and inline their own regex.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"

// scoreFuzzyMatch delegates to model.ScoreFuzzyMatch — previously defined in model_selection.go.
func scoreFuzzyMatch(query string, candidate model.ModelCandidate) int {
	return model.ScoreFuzzyMatch(query, candidate)
}
