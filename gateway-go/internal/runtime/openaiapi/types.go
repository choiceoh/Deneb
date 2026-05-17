package openaiapi

// ModelsList is the response body for GET /v1/models.
type ModelsList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// Model is a single entry in the OpenAI models list.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
