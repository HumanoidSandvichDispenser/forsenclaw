package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
)

const braveSearchToolID = "web_search"
const braveSearchBaseURL = "https://api.search.brave.com"

// BraveSearchClient calls Brave's web search API and exposes it as an MCP tool.
type BraveSearchClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewBraveSearch creates a Brave search MCP client using Brave's public API.
func NewBraveSearch(apiKey string) (*BraveSearchClient, error) {
	return newBraveSearchClient(braveSearchBaseURL, apiKey, http.DefaultClient)
}

func newBraveSearchClient(baseURL, apiKey string, httpClient *http.Client) (*BraveSearchClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("brave search api key is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = braveSearchBaseURL
	}
	return &BraveSearchClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: httpClient}, nil
}

func (c *BraveSearchClient) Call(ctx context.Context, toolID string, params map[string]string) (string, error) {
	if toolID != braveSearchToolID {
		return "", fmt.Errorf("unsupported tool %q", toolID)
	}

	query := strings.TrimSpace(params["query"])
	if query == "" {
		query = strings.TrimSpace(params["q"])
	}
	if query == "" {
		return "", fmt.Errorf("missing required parameter %q", "query")
	}

	endpoint, err := url.Parse(c.baseURL + "/res/v1/web/search")
	if err != nil {
		return "", fmt.Errorf("parse brave endpoint: %w", err)
	}
	q := endpoint.Query()
	q.Set("q", query)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build brave request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("brave search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("brave search request failed: %s", resp.Status)
	}

	var payload braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode brave search response: %w", err)
	}

	results := payload.Web.Results
	if len(results) == 0 {
		results = payload.Results
	}

	return formatBraveResults(query, results), nil
}

func (c *BraveSearchClient) ToolIDs() []string {
	return []string{braveSearchToolID}
}

func (c *BraveSearchClient) Healthy() bool {
	return strings.TrimSpace(c.apiKey) != ""
}

type braveSearchResponse struct {
	Web struct {
		Results []braveSearchResult `json:"results"`
	} `json:"web"`
	Results []braveSearchResult `json:"results"`
}

type braveSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func formatBraveResults(query string, results []braveSearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No Brave search results for %q.", query)
	}

	const maxResults = 5
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Brave search results for %q:\n", query))
	for i, result := range results {
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = "(untitled)"
		}
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))
		if result.URL != "" {
			b.WriteString(fmt.Sprintf("   URL: %s\n", result.URL))
		}
		if desc := strings.TrimSpace(result.Description); desc != "" {
			b.WriteString(fmt.Sprintf("   Snippet: %s\n", desc))
		}
	}

	return strings.TrimSpace(b.String())
}

func (c *BraveSearchClient) XMLSchemas() []string {
	return []string{`### web_search
Search the web using Brave Search.

Parameters:
- query (string, required): The search query.

Usage:
<tool_call>
  <tool_id>web_search</tool_id>
  <parameters>
    <query>search query here</query>
  </parameters>
</tool_call>`}
}

func (c *BraveSearchClient) NativeDefinitions() []inference.ToolDefinition {
	return []inference.ToolDefinition{
		{
			Name:        "web_search",
			Description: "Search the web using Brave Search.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query.",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

var _ mcp.MCPClient = (*BraveSearchClient)(nil)
var _ mcp.ToolDescriber = (*BraveSearchClient)(nil)
