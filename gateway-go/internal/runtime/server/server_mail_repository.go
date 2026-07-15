package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	handlerwire "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
)

// errNativeMailUnconfigured surfaces when the on-box mail archive isn't configured.
// The Gmail fallback was removed (operator decision): the miniapp mail surface is
// native-archive-only, so a missing archive is a clear "configure DENEB_ARCHIVE_IMAP_*"
// error rather than a silent switch to Gmail (which served wrong-source rows and
// dropped the per-mail AI analyses keyed by archive Message-IDs).
var errNativeMailUnconfigured = errors.New("native mail archive not configured (set DENEB_ARCHIVE_IMAP_ADDR/USER/PASS)")

func (s *Server) miniappMailClientFactory(denebDir string) func() (handlerwire.MailGmailClient, error) {
	return func() (handlerwire.MailGmailClient, error) {
		client, err := s.newMiniappMailClient(denebDir)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

func (s *Server) newMiniappMailClient(denebDir string) (handlerwire.MailGmailClient, error) {
	// Native-archive-only — no Gmail fallback (see errNativeMailUnconfigured).
	if repo := s.newArchiveMailRepository(denebDir, nil); repo != nil {
		return repo, nil
	}
	return nil, errNativeMailUnconfigured
}

func (s *Server) newMiniappMailAttachmentClient() (svcbind.MailAttachmentClient, error) {
	if repo := s.newArchiveMailRepository(s.denebDir, nil); repo != nil {
		return repo, nil
	}
	return nil, errNativeMailUnconfigured
}

func (s *Server) newArchiveMailRepository(denebDir string, fallback platbind.FallbackClient) *platbind.Repository {
	cfg := miniappArchiveMailConfig()
	if strings.TrimSpace(cfg.Addr) == "" ||
		strings.TrimSpace(cfg.User) == "" ||
		strings.TrimSpace(cfg.Pass) == "" {
		return nil
	}
	if denebDir == "" {
		denebDir = svcbind.DenebDir()
	}
	return platbind.NewRepository(cfg, platbind.RepositoryOptions{
		StatePath: filepath.Join(denebDir, "mail", "native_state.json"),
		Fallback:  fallback,
	})
}

func miniappArchiveMailConfig() platbind.MailArchiveConfig {
	return platbind.MailArchiveConfig{
		Addr:      archiveIMAPAddr(),
		User:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_USER")),
		Pass:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_PASS")),
		Mailboxes: archiveIMAPMailboxes(),
		Timeout:   8 * time.Second,
	}
}

func archiveIMAPMailboxes() []string {
	return platbind.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
}
