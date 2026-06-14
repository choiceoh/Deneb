// privacy.go — local-first egress guard. A wormhole that fronts both local and
// cloud models is a place where one routing slip could send private data
// (mail, contacts) to a cloud provider. This lets a sensitive caller — or the
// whole instance — guarantee a request never leaves the box.
package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// isLocalURL reports whether an upstream URL points at this machine or a private
// network — i.e. a request to it stays under the operator's control. A public
// hostname (anything that isn't a loopback/private/link-local IP or "localhost")
// is treated as cloud.
func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false // a public (or unresolved) hostname → treat as cloud
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// isLocal reports whether a model's backend keeps data on-box: the explicit
// config override wins, else it's auto-detected from the upstream URL.
func (e modelEntry) isLocal() bool {
	if e.Local != nil {
		return *e.Local
	}
	return isLocalURL(e.URL)
}

// isLoopbackListen reports whether a listen address binds only the loopback
// interface (so it is unreachable off-box). Used to warn loudly when wormhole
// binds a routable address (tailnet / all interfaces) without a token — i.e. it
// is OPEN to the network. A bare ":18800" (empty host) binds all interfaces.
func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		host = strings.TrimSpace(listen) // tolerate a host with no port
	}
	if host == "" {
		return false // ":18800" / "" = all interfaces, NOT loopback
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// localOnly reports whether THIS request must stay local — either the instance
// is in local-only mode, or the caller set the X-Wormhole-Local-Only header to
// guarantee no cloud egress for a sensitive payload.
func (rt *router) localOnly(r *http.Request) bool {
	if rt.cur().cfg.LocalOnly {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("X-Wormhole-Local-Only"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
