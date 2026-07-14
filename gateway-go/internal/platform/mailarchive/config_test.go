package mailarchive

import "testing"

func TestAddressFromEnvReturnsTrimmedOverrideOrDefault(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_IMAP_ADDR", " mail.example:1143 ")
	if got := AddressFromEnv(); got != "mail.example:1143" {
		t.Fatalf("AddressFromEnv override = %q", got)
	}
	t.Setenv("DENEB_ARCHIVE_IMAP_ADDR", "")
	if got := AddressFromEnv(); got != defaultAddress {
		t.Fatalf("AddressFromEnv default = %q, want %q", got, defaultAddress)
	}
}
