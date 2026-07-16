package lmtpd

import (
	"net"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/sdsocket"
)

// systemdListener returns a listener built from the systemd socket-activated
// file descriptor named "lmtp" (FileDescriptorName in deneb-lmtp.socket). ok is
// false when this process was not socket-activated for it — the caller then
// binds the socket itself.
//
// Why this exists: systemd holds the listening socket across the gateway's
// frequent SIGUSR1 hot-restarts (every auto-deploy). With socket activation,
// mail that arrives during the ~10s restart window queues in the kernel backlog
// and is accepted when the new process picks the fd back up — instead of getting
// "connection refused", which the upstream Maddy queue misclassifies as a
// permanent error and drops (a real forwarded mail was lost this way on
// 2026-06-16). Without a socket unit (LISTEN_* unset) this is a no-op.
//
// The sd_listen_fds protocol handling lives in infra/sdsocket, shared with the
// HTTP listener — the activation env is captured once there, so the two
// consumers cannot erase each other's environment.
func systemdListener(name string) (net.Listener, bool) {
	return sdsocket.Listener(name)
}
