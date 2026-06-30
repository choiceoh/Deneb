package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

// errNativeMailUnconfigured surfaces when the on-box mail archive isn't configured.
// The Gmail fallback was removed (operator decision): the miniapp mail surface is
// native-archive-only, so a missing archive is a clear "configure DENEB_ARCHIVE_IMAP_*"
// error rather than a silent switch to Gmail (which served wrong-source rows and
// dropped the per-mail AI analyses keyed by archive Message-IDs).
var errNativeMailUnconfigured = errors.New("native mail archive not configured (set DENEB_ARCHIVE_IMAP_ADDR/USER/PASS)")

func (s *Server) miniappMailClientFactory(denebDir string) func() (handlerminiapp.GmailClient, error) {
	return func() (handlerminiapp.GmailClient, error) {
		client, err := s.newMiniappMailClient(denebDir)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

func (s *Server) newMiniappMailClient(denebDir string) (handlerminiapp.GmailClient, error) {
	// Native-archive-only — no Gmail fallback (see errNativeMailUnconfigured).
	if repo := s.newArchiveMailRepository(denebDir, nil); repo != nil {
		return repo, nil
	}
	return nil, errNativeMailUnconfigured
}

func (s *Server) newMiniappMailAttachmentClient() (miniappMailAttachmentClient, error) {
	if repo := s.newArchiveMailRepository(s.denebDir, nil); repo != nil {
		return repo, nil
	}
	return nil, errNativeMailUnconfigured
}

func (s *Server) newArchiveMailRepository(denebDir string, fallback mailarchive.FallbackClient) *mailarchive.Repository {
	cfg := miniappArchiveMailConfig()
	if strings.TrimSpace(cfg.Addr) == "" ||
		strings.TrimSpace(cfg.User) == "" ||
		strings.TrimSpace(cfg.Pass) == "" {
		return nil
	}
	if denebDir == "" {
		denebDir = resolveDenebDir()
	}
	return mailarchive.NewRepository(cfg, mailarchive.RepositoryOptions{
		StatePath: filepath.Join(denebDir, "mail", "native_state.json"),
		Fallback:  fallback,
	})
}

func miniappArchiveMailConfig() mailarchive.Config {
	return mailarchive.Config{
		Addr:      archiveIMAPAddr(),
		User:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_USER")),
		Pass:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_PASS")),
		Mailboxes: archiveIMAPMailboxes(),
		Timeout:   8 * time.Second,
	}
}

func archiveIMAPMailboxes() []string {
	return mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
}
