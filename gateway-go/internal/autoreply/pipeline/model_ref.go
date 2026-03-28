package pipeline

// SplitProviderModel splits a "provider/model" reference into [provider, model].
// If no provider is given, provider is empty and model is returned as-is.
func SplitProviderModel(ref string) [2]string {
	idx := -1
	for i, c := range ref {
		if c == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return [2]string{"", ref}
	}
	return [2]string{ref[:idx], ref[idx+1:]}
}
