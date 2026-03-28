package shortid

import "time"

const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// New returns "prefix_<base62-nanosecond-timestamp>".
// Example: "run_2kFp8Qm1xZ3" (~15 chars vs ~23 with decimal).
func New(prefix string) string {
	return prefix + "_" + encodeBase62(time.Now().UnixNano())
}

func encodeBase62(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [11]byte // max 11 chars for int64 in base62
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = charset[n%62]
		n /= 62
	}
	return string(buf[i:])
}
