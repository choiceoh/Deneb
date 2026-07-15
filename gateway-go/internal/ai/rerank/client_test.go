package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRestoresResultIndexOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var request rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "model-a" || len(request.Documents) != 2 {
			t.Fatalf("request = %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "model-a")
	scores, err := client.Rerank(context.Background(), "query", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[0] != 0.1 || scores[1] != 0.9 {
		t.Fatalf("scores = %v", scores)
	}
}

func TestNewFromEnvRequiresExplicitURL(t *testing.T) {
	t.Setenv("DENEB_RERANK_URL", "")
	if client := NewFromEnv(); client != nil {
		t.Fatalf("client = %#v, want nil", client)
	}
}
