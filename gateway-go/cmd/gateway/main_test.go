package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestSmokeHealthEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := testutil.Must(server.New("127.0.0.1:0"))
	addr := testutil.Must(srv.StartAndListen(ctx))
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/health", addr.String())
	req := testutil.Must(http.NewRequestWithContext(ctx, http.MethodGet, url, nil))
	resp := testutil.Must(http.DefaultClient.Do(req))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

