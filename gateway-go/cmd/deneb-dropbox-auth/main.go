// Command deneb-dropbox-auth runs the one-time Dropbox OAuth2 PKCE flow to mint
// a long-lived refresh token for the gateway. Run once on the gateway host:
//
//	go run ./cmd/deneb-dropbox-auth -app-key <APP_KEY>
//
// It prints a consent URL; open it in a browser, approve, and paste the shown
// authorization code back. The resulting refresh token is written to
// ~/.deneb/credentials/dropbox_token.json (0600), which enables the dropbox
// chat tool. PKCE is used by default, so no app secret needs to live on the
// host (pass -app-secret only for a confidential Dropbox app).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
)

func main() {
	appKey := flag.String("app-key", "", "Dropbox app key (from the App Console). Reuses dropbox_app.json if omitted.")
	appSecret := flag.String("app-secret", "", "Dropbox app secret (optional; PKCE is used when empty)")
	dir := flag.String("dir", dropbox.CredentialsDir(), "credentials directory")
	flag.Parse()

	key := strings.TrimSpace(*appKey)
	secret := strings.TrimSpace(*appSecret)

	// Fall back to an existing dropbox_app.json, then prompt interactively.
	if key == "" {
		if k, s, ok := dropbox.LoadApp(*dir); ok {
			key, secret = k, s
		}
	}
	if key == "" {
		key = prompt("Dropbox App key를 입력하세요: ")
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "error: app key가 필요합니다")
		os.Exit(1)
	}

	verifier, challenge, err := dropbox.GeneratePKCE()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	authURL := dropbox.AuthorizeURL(key, challenge, dropbox.DefaultScopes)
	fmt.Fprintln(os.Stderr, "1) 아래 URL을 브라우저에서 열고 Deneb 앱 접근을 승인하세요:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "   "+authURL)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "2) 화면에 표시된 인증 코드를 아래에 붙여넣으세요.")

	code := prompt("인증 코드: ")
	if code == "" {
		fmt.Fprintln(os.Stderr, "error: 인증 코드가 비어 있습니다")
		os.Exit(1)
	}

	tr, err := dropbox.ExchangeCode(context.Background(), key, secret, code, verifier)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if err := dropbox.SaveApp(*dir, key, secret); err != nil {
		fmt.Fprintln(os.Stderr, "error: 앱 정보 저장 실패:", err)
		os.Exit(1)
	}
	if err := dropbox.SaveToken(*dir, tr); err != nil {
		fmt.Fprintln(os.Stderr, "error: 토큰 저장 실패:", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n✓ Dropbox 연동 완료. 토큰을 %s 에 저장했습니다.\n", *dir)
	fmt.Fprintln(os.Stderr, "  이제 채팅에서 dropbox 도구를 사용할 수 있습니다.")
}

// prompt writes a label to stderr and reads one trimmed line from stdin.
func prompt(label string) string {
	fmt.Fprint(os.Stderr, label)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}
