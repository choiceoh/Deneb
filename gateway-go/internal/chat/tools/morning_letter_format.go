package tools

import "context"

// CollectMorningLetterData runs the 5-section data collection (weather,
// exchange, copper, calendar, email) in parallel and returns the raw JSON
// string. This is the same data that ToolMorningLetter returns, but callable
// directly without going through the LLM tool-call loop.
func CollectMorningLetterData(ctx context.Context) (string, error) {
	toolFn := ToolMorningLetter(nil)
	return toolFn(ctx, nil)
}
