package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
)

const webFetchToolID = "webfetch"

const webFetchMaxBytes = 10 << 20

// WebFetchClient fetches a web page and extracts its main readable content.
type WebFetchClient struct {
	httpClient *http.Client
}

// NewWebFetch creates the built-in webfetch MCP tool.
func NewWebFetch() *WebFetchClient {
	return newWebFetchClient(http.DefaultClient)
}

func newWebFetchClient(httpClient *http.Client) *WebFetchClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &WebFetchClient{httpClient: httpClient}
}

func (c *WebFetchClient) Call(ctx context.Context, toolID string, params map[string]string) (string, error) {
	if toolID != webFetchToolID {
		return "", fmt.Errorf("unsupported tool %q", toolID)
	}

	rawURL := strings.TrimSpace(params["url"])
	if rawURL == "" {
		return "", fmt.Errorf("missing required parameter %q", "url")
	}

	structured, err := parseBoolParam(params["structure"])
	if err != nil {
		return "", fmt.Errorf("invalid structure parameter: %w", err)
	}

	pageURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch url failed: %s", resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !isHTMLContentType(contentType) {
		return "", fmt.Errorf("unsupported content-type %q", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBytes))
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	finalURL := pageURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL
	}

	article, err := readability.FromReader(bytes.NewReader(body), finalURL)
	if err != nil {
		return "", fmt.Errorf("extract readable content: %w", err)
	}

	var content string
	if structured {
		var buf bytes.Buffer
		if err := article.RenderHTML(&buf); err != nil {
			return "", fmt.Errorf("render html: %w", err)
		}
		content = buf.String()
	} else {
		var buf bytes.Buffer
		if err := article.RenderText(&buf); err != nil {
			return "", fmt.Errorf("render text: %w", err)
		}
		content = buf.String()
	}

	return formatWebFetchResult(article.Title(), finalURL.String(), content), nil
}

func (c *WebFetchClient) ToolIDs() []string {
	return []string{webFetchToolID}
}

func (c *WebFetchClient) Healthy() bool {
	return true
}

func parseBoolParam(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func isHTMLContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml") || strings.Contains(contentType, "xml")
}

func formatWebFetchResult(title, sourceURL, content string) string {
	var b strings.Builder
	if title != "" {
		b.WriteString("Title: ")
		b.WriteString(strings.TrimSpace(title))
		b.WriteString("\n")
	}
	if sourceURL != "" {
		b.WriteString("URL: ")
		b.WriteString(strings.TrimSpace(sourceURL))
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(content))
	return strings.TrimSpace(b.String())
}

var _ mcp.MCPClient = (*WebFetchClient)(nil)
