package handlerminiapp

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mapFilesError must map each filestore error class to the right RPC code:
// a missing path → NOT_FOUND, a path-escape attempt (a malformed client path)
// → INVALID_REQUEST (a 4xx client error, not a retryable server fault), and any
// other failure → UNAVAILABLE.
func TestMapFilesError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, protocol.ErrUnavailable},
		{"not-exist", fs.ErrNotExist, protocol.ErrNotFound},
		{"wrapped-not-exist", fmt.Errorf("stat: %w", fs.ErrNotExist), protocol.ErrNotFound},
		{"path-escape", filestore.ErrPathEscape, protocol.ErrInvalidRequest},
		{"wrapped-path-escape", fmt.Errorf("resolve: %w", filestore.ErrPathEscape), protocol.ErrInvalidRequest},
		{"other", errors.New("disk on fire"), protocol.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := mapFilesError("req-1", "file op failed", tc.err)
			if resp == nil || resp.Error == nil {
				t.Fatalf("got nil error response")
			}
			if resp.Error.Code != tc.want {
				t.Errorf("code = %q, want %q", resp.Error.Code, tc.want)
			}
		})
	}
}
