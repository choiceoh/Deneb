// Package sdsocket resolves systemd socket-activated listeners (the
// sd_listen_fds(3) protocol) by FileDescriptorName.
//
// Why this exists: systemd holds a listening socket across the gateway's
// frequent SIGUSR1 hot-restarts (every auto-deploy), so traffic that arrives
// during the restart window queues in the kernel backlog instead of getting
// "connection refused". The LMTP ingest socket adopted this first (a real
// forwarded mail was lost to a refused window on 2026-06-16); the HTTP
// listener is the second consumer.
//
// The activation environment is read and cleared ONCE at first use and cached
// for the process lifetime, so multiple consumers in one process (lmtp + http)
// each claim their own fd regardless of call order — with per-consumer env
// clearing, whichever ran first would have erased the other's environment.
// Each fd can be claimed once; a duplicate claim returns ok=false.
package sdsocket

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// listenFdsStart is SD_LISTEN_FDS_START: systemd passes activated sockets as
// file descriptors numbered from 3 upward.
const listenFdsStart = 3

// activation is the parsed LISTEN_* snapshot taken at first use.
type activation struct {
	nfds  int
	names []string
}

var (
	captureOnce sync.Once
	act         activation

	claimMu sync.Mutex
	claimed map[int]bool
)

func capture() {
	act = parseActivation(os.Getpid(),
		os.Getenv("LISTEN_PID"), os.Getenv("LISTEN_FDS"), os.Getenv("LISTEN_FDNAMES"))
	claimed = make(map[int]bool)
	// Clear so exec'd subprocesses don't inherit a stale activation environment
	// (matches sd_listen_fds(unset_environment=1)). The parsed snapshot above is
	// the process-lifetime source of truth for every later consumer.
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
}

// Listener returns a listener built from the systemd socket-activated file
// descriptor whose FileDescriptorName matches name. ok is false when this
// process was not socket-activated for name — the caller should then bind the
// socket itself (the whole mechanism is opt-in via the socket unit).
func Listener(name string) (net.Listener, bool) {
	captureOnce.Do(capture)
	fd, ok := selectFD(act, name)
	if !ok {
		return nil, false
	}
	claimMu.Lock()
	dup := claimed[fd]
	claimed[fd] = true
	claimMu.Unlock()
	if dup {
		return nil, false // two consumers resolving to one fd would fight over accepts
	}
	f := os.NewFile(uintptr(fd), "sd-socket-"+name)
	if f == nil {
		return nil, false
	}
	ln, err := net.FileListener(f) // dups fd; the dup is close-on-exec
	_ = f.Close()                  // drop our extra reference (does not affect ln)
	if err != nil {
		return nil, false
	}
	return ln, true
}

// parseActivation validates the raw LISTEN_* env against pid and returns the
// fd-count/name snapshot (zero value when not activated for this process).
// Pure, so the protocol logic is unit-testable without real fds.
func parseActivation(pid int, listenPid, listenFds, listenFdNames string) activation {
	if listenPid == "" || listenFds == "" {
		return activation{}
	}
	if p, err := strconv.Atoi(listenPid); err != nil || p != pid {
		return activation{} // the fds were meant for a different process
	}
	nfds, err := strconv.Atoi(listenFds)
	if err != nil || nfds < 1 {
		return activation{}
	}
	var names []string
	if listenFdNames != "" {
		names = strings.Split(listenFdNames, ":")
	}
	return activation{nfds: nfds, names: names}
}

// selectFD resolves which inherited fd carries name, or ok=false. A lone
// unnamed fd is accepted for any name (single-socket units without
// FileDescriptorName) — the claim guard in Listener keeps two consumers from
// both taking it.
func selectFD(a activation, name string) (int, bool) {
	for i := 0; i < a.nfds; i++ {
		if i < len(a.names) && a.names[i] == name {
			return listenFdsStart + i, true
		}
	}
	if a.nfds == 1 && len(a.names) == 0 {
		return listenFdsStart, true
	}
	return 0, false
}
