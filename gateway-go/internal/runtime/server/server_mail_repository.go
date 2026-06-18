package server

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

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
	fallback, gmailErr := gmail.DefaultClient()
	var fallbackClient mailarchive.FallbackClient
	if gmailErr == nil && fallback != nil {
		fallbackClient = fallback
	}
	if repo := s.newArchiveMailRepository(denebDir, fallbackClient); repo != nil {
		return repo, nil
	}
	if gmailErr != nil {
		return nil, gmailErr
	}
	return fallback, nil
}

func (s *Server) newMiniappMailAttachmentClient() (miniappMailAttachmentClient, error) {
	fallback, gmailErr := gmail.DefaultClient()
	var fallbackClient mailarchive.FallbackClient
	if gmailErr == nil && fallback != nil {
		fallbackClient = fallback
	}
	if repo := s.newArchiveMailRepository(s.denebDir, fallbackClient); repo != nil {
		return repo, nil
	}
	if gmailErr != nil {
		return nil, gmailErr
	}
	return fallback, nil
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
