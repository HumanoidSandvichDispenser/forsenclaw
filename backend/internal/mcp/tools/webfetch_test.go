package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchClientPlaintext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/article", http.StatusFound)
			return
		}
		if r.URL.Path != "/article" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
<!doctype html>
<html>
  <head><title>Ignored</title></head>
  <body>
    <article>
      <h1>Readable Title</h1>
      <p>Readable content here.</p>
    </article>
  </body>
</html>`))
	}))
	defer server.Close()

	client := newWebFetchClient(server.Client(), 0)
	got, err := client.Call(context.Background(), webFetchToolID, map[string]string{"url": server.URL + "/start"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(got, "Readable Title") {
		t.Fatalf("expected title in output, got %q", got)
	}
	if !strings.Contains(got, "Readable content here.") {
		t.Fatalf("expected article text in output, got %q", got)
	}
	if strings.Contains(got, "<p>") {
		t.Fatalf("expected plaintext output, got %q", got)
	}
	if !strings.Contains(got, "/article") {
		t.Fatalf("expected final URL in output, got %q", got)
	}
}

func TestWebFetchClientStructured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
<!doctype html>
<html>
  <body>
    <article>
      <h1>Readable Title</h1>
      <p><a href="/relative">Link</a></p>
    </article>
  </body>
</html>`))
	}))
	defer server.Close()

	client := newWebFetchClient(server.Client(), 0)
	got, err := client.Call(context.Background(), webFetchToolID, map[string]string{"url": server.URL + "/article", "structure": "true"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(got, "<p>") {
		t.Fatalf("expected structured HTML output, got %q", got)
	}
	if !strings.Contains(got, "Readable Title") {
		t.Fatalf("expected title in output, got %q", got)
	}
}

func TestWebFetchClientMissingURL(t *testing.T) {
	client := newWebFetchClient(http.DefaultClient, 0)
	if _, err := client.Call(context.Background(), webFetchToolID, map[string]string{}); err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestWebFetchClientInvalidStructure(t *testing.T) {
	client := newWebFetchClient(http.DefaultClient, 0)
	if _, err := client.Call(context.Background(), webFetchToolID, map[string]string{"url": "https://example.com", "structure": "maybe"}); err == nil {
		t.Fatal("expected error for invalid structure bool")
	}
}
