// Command deneb-client-token generates (or rotates) the standalone native-client
// auth secret in {stateDir}/client_token and prints it for one-time pairing into
// the native app. Run once on the gateway host:
//
//	go run ./cmd/deneb-client-token
//
// Generating the token is what enables standalone-client auth; until then the
// gateway rejects native-client RPCs as unauthenticated. Keep the printed value secret.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func main() {
	token, err := clientauth.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	writePairingOutput(os.Stdout, os.Stderr, token)
}

// writePairingOutput keeps the secret pipeable on stdout while all operator
// guidance stays on stderr.
func writePairingOutput(stdout, stderr io.Writer, token string) {
	fmt.Fprintln(stdout, token)
	fmt.Fprintf(stderr, "Wrote %s secret (0600). Paste this value into the native app to pair.\n", clientauth.Header)
}
