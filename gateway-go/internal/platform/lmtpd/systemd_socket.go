package lmtpd

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// listenFdsStart is SD_LISTEN_FDS_START: systemd passes activated sockets as
// file descriptors numbered from 3 upward.
const listenFdsStart = 3

// systemdListener returns a listener built from a systemd socket-activated file
// descriptor whose FileDescriptorName matches name (the sd_listen_fds(3)
// protocol). ok is false when this process was not socket-activated for name —
// the caller should then bind the socket itself.
//
// Why this exists: systemd holds the listening socket across the gateway's
// frequent SIGUSR1 hot-restarts (every auto-deploy). With socket activation,
// mail that arrives during the ~10s restart window queues in the kernel backlog
// and is accepted when the new process picks the fd back up — instead of getting
// "connection refused", which the upstream Maddy queue misclassifies as a
// permanent error and drops (a real forwarded mail was lost this way on
// 2026-06-16). Without a socket unit (LISTEN_* unset) this is a no-op.
func systemdListener(name string) (net.Listener, bool) {
	fd, ok := selectListenFD(os.Getpid(), os.Getenv("LISTEN_PID"),
		os.Getenv("LISTEN_FDS"), os.Getenv("LISTEN_FDNAMES"), name)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(fd), "lmtp-systemd-socket")
	if f == nil {
		return nil, false
	}
	ln, err := net.FileListener(f) // dups fd; the dup is close-on-exec
	_ = f.Close()                  // drop our extra reference (does not affect ln)
	if err != nil {
		return nil, false
	}
	// Clear LISTEN_* so tool subprocesses we exec don't inherit a stale
	// activation environment (matches sd_listen_fds(unset_environment=1)).
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	return ln, true
}

// selectListenFD resolves which inherited fd to use for name, or ok=false. It is
// pure (no fd I/O) so the env-parsing logic is unit-testable. listenPid /
// listenFds / listenFdNames are the raw LISTEN_PID / LISTEN_FDS /
// LISTEN_FDNAMES env values.
func selectListenFD(pid int, listenPid, listenFds, listenFdNames, name string) (int, bool) {
	if listenPid == "" || listenFds == "" {
		return 0, false
	}
	if p, err := strconv.Atoi(listenPid); err != nil || p != pid {
		return 0, false // the fds were meant for a different process
	}
	nfds, err := strconv.Atoi(listenFds)
	if err != nil || nfds < 1 {
		return 0, false
	}
	names := strings.Split(listenFdNames, ":")
	for i := 0; i < nfds; i++ {
		if i < len(names) && names[i] == name {
			return listenFdsStart + i, true
		}
	}
	// No name match: accept a lone fd (single-socket activation, no name set).
	if nfds == 1 {
		return listenFdsStart, true
	}
	return 0, false
}
