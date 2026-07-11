package nativeauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func TestAuthenticateHeaderAndDownloadQuery(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}

	headerReq := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	headerReq.Header.Set(clientauth.Header, token)
	if identity, ok := Authenticate(httptest.NewRecorder(), headerReq, nil); !ok || identity == nil || identity.User == nil {
		t.Fatalf("valid header token did not produce an operator identity: %#v, %v", identity, ok)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/download?clientToken="+token, nil)
	if identity, ok := AuthenticateDownload(httptest.NewRecorder(), downloadReq, nil); !ok || identity == nil {
		t.Fatalf("valid download token did not authenticate: %#v, %v", identity, ok)
	}

	recorder := httptest.NewRecorder()
	if _, ok := Authenticate(recorder, httptest.NewRequest(http.MethodPost, "/rpc", nil), nil); ok || recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing token = ok %v, status %d; want false/401", ok, recorder.Code)
	}
}
