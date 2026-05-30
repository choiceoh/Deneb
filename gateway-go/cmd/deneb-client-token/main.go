// Command deneb-client-token generates (or rotates) the standalone native-client
// auth secret in {stateDir}/client_token and prints it for one-time pairing into
// the native app. Run once on the gateway host:
//
//	go run ./cmd/deneb-client-token
//
// Generating the token is what enables standalone-client auth; until then the
// gateway only accepts Telegram Mini App initData. Keep the printed value secret.
package main

import (
	"fmt"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func main() {
	token, err := clientauth.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// Token to stdout (pipeable); guidance to stderr.
	fmt.Println(token)
	fmt.Fprintf(os.Stderr, "Wrote %s secret (0600). Paste this value into the native app to pair.\n", clientauth.Header)
}
