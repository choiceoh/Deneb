package autoreply

import "strings"

// SendPolicy controls whether outbound messages are delivered.
type SendPolicy string

const (
	SendPolicyOn      SendPolicy = "on"
	SendPolicyOff     SendPolicy = "off"
	SendPolicyInherit SendPolicy = "inherit"
)

// NormalizeSendPolicy validates and normalizes a send policy string.
func NormalizeSendPolicy(raw string) (SendPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "yes", "1", "enable", "enabled":
		return SendPolicyOn, true
	case "off", "false", "no", "0", "disable", "disabled":
		return SendPolicyOff, true
	case "inherit", "default", "":
		return SendPolicyInherit, true
	}
	return "", false
}

// IsSendAllowed returns true if the effective send policy allows sending.
func IsSendAllowed(policy SendPolicy, parentPolicy SendPolicy) bool {
	switch policy {
	case SendPolicyOff:
		return false
	case SendPolicyOn:
		return true
	case SendPolicyInherit, "":
		return parentPolicy != SendPolicyOff
	}
	return true
}
