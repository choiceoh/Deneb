// Package mailarchive reads related mail from the on-box archive IMAP store
// (the deneb-mailarchive Maddy instance) so the email-analysis pipeline can
// reconstruct a message's thread and the sender's recent history locally,
// without depending on Gmail. It speaks just enough IMAP4rev1 (LOGIN, SELECT,
// UID SEARCH, UID FETCH, LOGOUT) for that read-only use — the archive is a
// trusted loopback peer (plaintext, single account), so there is no TLS, SASL,
// or general-purpose IMAP surface here on purpose.
package mailarchive

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// imapConn is a minimal IMAP4rev1 client connection. Not safe for concurrent use.
type imapConn struct {
	conn net.Conn
	r    *bufio.Reader
	tag  int
	dl   time.Duration
}

// dialIMAP connects and consumes the server greeting. addr is host:port.
func dialIMAP(ctx context.Context, addr string, timeout time.Duration) (*imapConn, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}
	c := &imapConn{conn: conn, r: bufio.NewReader(conn), dl: timeout}
	c.touchDeadline()
	// Greeting: "* OK ...".
	line, err := c.r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("imap greeting read: %w", err)
	}
	if !strings.HasPrefix(line, "* OK") {
		conn.Close()
		return nil, fmt.Errorf("imap greeting: %q", strings.TrimSpace(line))
	}
	return c, nil
}

func (c *imapConn) touchDeadline() {
	if c.dl > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(c.dl))
	}
}

func (c *imapConn) close() { _ = c.conn.Close() }

// nextTag returns a fresh command tag like "a3".
func (c *imapConn) nextTag() string {
	c.tag++
	return "a" + strconv.Itoa(c.tag)
}

// literalRe matches a trailing IMAP literal size marker: ... {1234}
var literalRe = regexp.MustCompile(`\{(\d+)\}\r?\n$`)

// readResponse sends a command and accumulates untagged response lines until the
// tagged completion line for tag. Literals ({n}) are read inline and appended to
// the current logical line as raw bytes, so a caller can pull message bodies out
// of FETCH responses. Returns the untagged lines and the completion status word
// ("OK"/"NO"/"BAD").
func (c *imapConn) exec(cmd string) (untagged [][]byte, status string, err error) {
	tag := c.nextTag()
	c.touchDeadline()
	if _, err = fmt.Fprintf(c.conn, "%s %s\r\n", tag, cmd); err != nil {
		return nil, "", fmt.Errorf("imap write: %w", err)
	}
	tagPrefix := tag + " "
	for {
		c.touchDeadline()
		line, rerr := c.r.ReadString('\n')
		if rerr != nil {
			return nil, "", fmt.Errorf("imap read: %w", rerr)
		}
		// Tagged completion line ends the command.
		if strings.HasPrefix(line, tagPrefix) {
			rest := strings.TrimSpace(line[len(tagPrefix):])
			fields := strings.SplitN(rest, " ", 2)
			return untagged, strings.ToUpper(fields[0]), nil
		}
		// An untagged line may carry a literal: read the announced bytes and
		// fold them (plus the continuation) into one logical entry.
		buf := []byte(line)
		for {
			m := literalRe.FindSubmatch(buf)
			if m == nil {
				break
			}
			n, _ := strconv.Atoi(string(m[1]))
			lit := make([]byte, n)
			c.touchDeadline()
			if _, rerr := readFull(c.r, lit); rerr != nil {
				return nil, "", fmt.Errorf("imap literal read: %w", rerr)
			}
			buf = append(buf, lit...)
			// Read the continuation of the line after the literal.
			cont, rerr := c.r.ReadString('\n')
			if rerr != nil {
				return nil, "", fmt.Errorf("imap literal cont: %w", rerr)
			}
			buf = append(buf, cont...)
		}
		untagged = append(untagged, buf)
	}
}

func readFull(r *bufio.Reader, p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := r.Read(p[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// login authenticates with LOGIN. user/pass are quoted; the archive account has
// no exotic characters, but quote-escape defensively.
func (c *imapConn) login(user, pass string) error {
	_, status, err := c.exec(fmt.Sprintf("LOGIN %s %s", quote(user), quote(pass)))
	if err != nil {
		return err
	}
	if status != "OK" {
		return fmt.Errorf("imap login rejected: %s", status)
	}
	return nil
}

// selectMailbox opens a mailbox read-only (EXAMINE) — we never mutate the archive.
func (c *imapConn) examine(mailbox string) error {
	_, status, err := c.exec("EXAMINE " + quote(mailbox))
	if err != nil {
		return err
	}
	if status != "OK" {
		return fmt.Errorf("imap examine %q: %s", mailbox, status)
	}
	return nil
}

// uidSearch runs UID SEARCH with the given criteria and returns matching UIDs.
func (c *imapConn) uidSearch(criteria string) ([]string, error) {
	untagged, status, err := c.exec("UID SEARCH " + criteria)
	if err != nil {
		return nil, err
	}
	if status != "OK" {
		return nil, fmt.Errorf("imap search rejected: %s", status)
	}
	var uids []string
	for _, ln := range untagged {
		s := strings.TrimSpace(string(ln))
		// "* SEARCH 1 2 3" (or "* SEARCH" when empty).
		if rest, ok := strings.CutPrefix(s, "* SEARCH"); ok {
			uids = append(uids, strings.Fields(rest)...)
		}
	}
	return uids, nil
}

// uidFetchBodies fetches full message bodies (BODY.PEEK[]) for the given UID set
// (e.g. "1,4,9"). Returns raw RFC822 bytes per message (UID mapping is not
// needed by callers — they dedup by Message-ID after parsing).
func (c *imapConn) uidFetchBodies(uidSet string) ([][]byte, error) {
	if strings.TrimSpace(uidSet) == "" {
		return nil, nil
	}
	untagged, status, err := c.exec(fmt.Sprintf("UID FETCH %s (UID BODY.PEEK[])", uidSet))
	if err != nil {
		return nil, err
	}
	if status != "OK" {
		return nil, fmt.Errorf("imap fetch rejected: %s", status)
	}
	var bodies [][]byte
	for _, entry := range untagged {
		// Each FETCH entry holds one literal (the body). Find where the literal
		// payload starts: just after the first "{n}" marker's CRLF.
		body, ok := extractLiteralPayload(entry)
		if ok {
			bodies = append(bodies, body)
		}
	}
	return bodies, nil
}

var anyLiteralRe = regexp.MustCompile(`\{(\d+)\}\r?\n`)

// extractLiteralPayload pulls the first literal's raw bytes out of a folded FETCH
// entry produced by exec (which appended the literal bytes right after "{n}\r\n").
func extractLiteralPayload(entry []byte) ([]byte, bool) {
	loc := anyLiteralRe.FindSubmatchIndex(entry)
	if loc == nil {
		return nil, false
	}
	n, _ := strconv.Atoi(string(entry[loc[2]:loc[3]]))
	start := loc[1] // just past "{n}\r\n" — where the literal payload begins
	if start > len(entry) {
		return nil, false
	}
	if n <= 0 || start+n > len(entry) {
		n = len(entry) - start // tolerate a short/over-announced literal
	}
	if n <= 0 {
		return nil, false
	}
	return entry[start : start+n], true
}

func (c *imapConn) logout() {
	_, _, _ = c.exec("LOGOUT")
}

// quote wraps a string as an IMAP quoted-string.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
