package tools

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBraveSearchClientCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/res/v1/web/search" {
			t.Fatalf("unexpected path: %s", got)
		}
		if got := r.URL.Query().Get("q"); got != "brave search" {
			t.Fatalf("unexpected query: %q", got)
		}
		if got := r.Header.Get("X-Subscription-Token"); got != "test-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "web": {
    "results": [
      {
        "title": "Brave Search",
        "url": "https://search.brave.com/",
        "description": "Privacy-first search engine"
      }
    ]
  }
}`))
	}))
	defer server.Close()

	client, err := newBraveSearchClient(server.URL, "test-key", server.Client())
	if err != nil {
		t.Fatalf("newBraveSearchClient: %v", err)
	}

	got, err := client.Call(context.Background(), braveSearchToolID, map[string]string{"query": "brave search"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !bytes.Contains([]byte(got), []byte("Brave Search")) {
		t.Fatalf("expected result text to include title, got %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("https://search.brave.com/")) {
		t.Fatalf("expected result text to include url, got %q", got)
	}
}

func TestBraveSearchClientMissingQuery(t *testing.T) {
	client, err := newBraveSearchClient("http://example.com", "test-key", http.DefaultClient)
	if err != nil {
		t.Fatalf("newBraveSearchClient: %v", err)
	}

	if _, err := client.Call(context.Background(), braveSearchToolID, map[string]string{}); err == nil {
		t.Fatal("expected error for missing query")
	}
}
