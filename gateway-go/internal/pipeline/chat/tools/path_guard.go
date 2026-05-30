// path_guard.go — credential / control-plane file protection for fs tools.
//
// Deneb is single-user, so this is not about isolating tenants; it is
// defense-in-depth against prompt-injection. If an attacker plants instructions
// in content the agent ingests (a web page, an email), they may try to make the
// agent read a credential file and echo it to Telegram, or overwrite a
// control-plane file (auth/config) to escalate. ResolvePath already jails fs
// tools to the workspace, but the workspace can legitimately include the
// operator's home, so a path-suffix denylist is the layer that stops credential
// stores and control-plane files from being read or written through the agent.
//
// Hard-deny (returns an error) rather than warn: unlike exec — where the
// operator may legitimately need an escape hatch — there is no benign reason for
// the read/write/edit tools to touch these paths. Mirrors hermes-agent's
// control-plane write-protection + credential read-deny.
package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// protectedPathPatterns matches credential and control-plane files by path
// suffix (matched against the slash-normalized absolute path). High-precision:
// each alternative names a well-known secret store or Deneb control-plane file.
var protectedPathPatterns = regexp.MustCompile(`(?i)(?:` +
	// Deneb control plane: credentials dir, sessions, and top-level json/env
	// (deneb.json, auth.json, config) under ~/.deneb.
	`(?:^|/)\.deneb/credentials(?:/|$)|` +
	`(?:^|/)\.deneb/sessions(?:/|$)|` +
	`(?:^|/)\.deneb/[^/]*\.(?:json|env|token)$|` +
	// SSH and cloud-provider credential stores.
	`(?:^|/)\.ssh(?:/|$)|` +
	`(?:^|/)\.aws/credentials$|` +
	`(?:^|/)\.config/gcloud/|` +
	`(?:^|/)\.kube/config$|` +
	`(?:^|/)\.netrc$|` +
	`(?:^|/)\.npmrc$|` +
	`(?:^|/)\.pypirc$|` +
	// Private keys and dotenv secret files anywhere.
	`(?:^|/)id_(?:rsa|dsa|ecdsa|ed25519)(?:\.pub)?$|` +
	`(?:^|/)\.env(?:\.[^/]+)?$` +
	`)`)

// CheckProtectedPath returns a non-nil error if path is a credential or
// control-plane file that agent fs tools must not access. op is "read", "write",
// or "edit" — used only to make the refusal message actionable. path may be
// relative; it is resolved to an absolute, slash-normalized form before
// matching so "./.env" and "/home/user/.env" are treated identically.
func CheckProtectedPath(path, op string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if protectedPathPatterns.MatchString(filepath.ToSlash(abs)) {
		return fmt.Errorf(
			"access denied: %s is a protected credential/control-plane path and cannot be %sed through agent tools "+
				"(prompt-injection safeguard)", path, op)
	}
	return nil
}
