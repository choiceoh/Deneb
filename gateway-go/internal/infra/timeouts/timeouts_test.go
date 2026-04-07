package timeouts

import (
	"testing"
	"time"
)

func TestTimeoutConstants(t *testing.T) {
	// Ensure timeout values are reasonable and non-zero.
	cases := []struct {
		name string
		val  time.Duration
		min  time.Duration
		max  time.Duration
	}{
		{"RPCDispatch", RPCDispatch, 5 * time.Second, 5 * time.Minute},
		{"ProcessExec", ProcessExec, 10 * time.Second, 10 * time.Minute},
		{"ProviderHTTP", ProviderHTTP, 5 * time.Second, 5 * time.Minute},
		{"GracefulShutdown", GracefulShutdown, 1 * time.Second, 30 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.val < tc.min {
				t.Errorf("%s = %v, want >= %v", tc.name, tc.val, tc.min)
			}
			if tc.val > tc.max {
				t.Errorf("%s = %v, want <= %v", tc.name, tc.val, tc.max)
			}
		})
	}
}
