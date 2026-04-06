//go:build no_ffi || !cgo

// Hand-written constants. Previously generated from YAML.

package ffi

// blockedHosts are hostnames that should not be accessed (SSRF protection).
var blockedHosts = map[string]bool{
	"localhost":                true,
	"127.0.0.1":                true,
	"0.0.0.0":                  true,
	"[::1]":                    true,
	"::1":                      true,
	"metadata.google.internal": true,
	"169.254.169.254":          true,
}

// blockedSchemes are URL schemes that should never be followed.
var blockedSchemes = map[string]bool{
	"file":   true,
	"ftp":    true,
	"gopher": true,
	"dict":   true,
	"data":   true,
	"ldap":   true,
	"ldaps":  true,
	"tftp":   true,
	"telnet": true,
}
