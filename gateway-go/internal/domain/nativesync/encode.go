package nativesync

import "encoding/json"

func mustRawJSON(v any) rawJSON {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}
