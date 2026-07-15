package watchtool

import "testing"

func TestFormatWatchDurationClampsExternalNegativeValues(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{seconds: -1, want: "0:00"},
		{seconds: 0, want: "0:00"},
		{seconds: 65, want: "1:05"},
		{seconds: 3_661, want: "1:01:01"},
	} {
		if got := formatWatchDuration(tc.seconds); got != tc.want {
			t.Errorf("formatWatchDuration(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
