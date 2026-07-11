package mailarchive

import (
	"os"
	"strings"
)

const defaultAddress = "127.0.0.1:1143"

// AddressFromEnv returns the configured archive IMAP address or the local
// archive service default when DENEB_ARCHIVE_IMAP_ADDR is unset.
func AddressFromEnv() string {
	if address := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_ADDR")); address != "" {
		return address
	}
	return defaultAddress
}
