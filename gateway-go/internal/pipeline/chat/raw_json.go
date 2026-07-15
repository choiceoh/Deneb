package chat

import "encoding/json"

type rawJSON = json.RawMessage
type jsonObject = map[string]any

func mustRawJSON(v any) rawJSON {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}
