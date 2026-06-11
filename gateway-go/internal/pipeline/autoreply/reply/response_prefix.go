package reply

import (
	"time"
)

// ResponsePrefixTemplate defines the format for response prefix headers.
type ResponsePrefixTemplate struct {
	Format   string // Go template-like format
	Timezone string
}

// ResponsePrefixParams holds the values for response prefix formatting.
type ResponsePrefixParams struct {
	Timestamp time.Time
	Model     string
	Provider  string
	ElapsedMs int64
}
