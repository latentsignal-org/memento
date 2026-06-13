package msgvaultapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type SearchResult struct {
	ID             int64          `json:"id"`
	ConversationID int64          `json:"conversation_id,omitempty"`
	Subject        string         `json:"subject,omitempty"`
	Snippet        string         `json:"snippet,omitempty"`
	SentAt         string         `json:"sent_at,omitempty"`
	From           string         `json:"from,omitempty"`
	To             []string       `json:"to,omitempty"`
	CC             []string       `json:"cc,omitempty"`
	BCC            []string       `json:"bcc,omitempty"`
	Labels         []string       `json:"labels,omitempty"`
	Score          map[string]any `json:"score,omitempty"`
	Raw            map[string]any `json:"-"`
}

type SearchResponse struct {
	Query         string         `json:"query"`
	Mode          string         `json:"mode,omitempty"`
	Total         int            `json:"total,omitempty"`
	Returned      int            `json:"returned,omitempty"`
	PoolSaturated bool           `json:"pool_saturated,omitempty"`
	TookMS        int            `json:"took_ms,omitempty"`
	Messages      []SearchResult `json:"messages,omitempty"`
	Results       []SearchResult `json:"results,omitempty"`
}

func (r SearchResponse) Items() []SearchResult {
	if r.Results != nil {
		return r.Results
	}
	return r.Messages
}

type Message struct {
	ID                   int64    `json:"id"`
	ConversationID       int64    `json:"conversation_id,omitempty"`
	Subject              string   `json:"subject,omitempty"`
	From                 string   `json:"from,omitempty"`
	To                   []string `json:"to,omitempty"`
	CC                   []string `json:"cc,omitempty"`
	BCC                  []string `json:"bcc,omitempty"`
	SentAt               string   `json:"sent_at,omitempty"`
	Snippet              string   `json:"snippet,omitempty"`
	Body                 string   `json:"body,omitempty"`
	BodyText             string   `json:"body_text,omitempty"`
	SourceMessageID      string   `json:"source_message_id,omitempty"`
	SourceConversationID string   `json:"source_conversation_id,omitempty"`
	Labels               []string `json:"labels,omitempty"`
}

func (m Message) TextBody() string {
	if m.BodyText != "" {
		return m.BodyText
	}
	return m.Body
}

func FromEnv() (*Client, bool) {
	baseURL := strings.TrimSpace(os.Getenv("MEMENTO_MSGVAULT_API_URL"))
	if baseURL == "" {
		return nil, false
	}
	return New(baseURL, os.Getenv("MEMENTO_MSGVAULT_API_KEY")), true
}

func New(baseURL, apiKey string) *Client {
	return NewWithHTTPClient(baseURL, apiKey, &http.Client{Timeout: defaultTimeout})
}

func NewWithHTTPClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
	}
}

func (c *Client) Search(ctx context.Context, query, mode string, pageSize int, explain bool) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResponse{}, fmt.Errorf("query is required")
	}
	mode = normalizeMode(mode)
	if pageSize <= 0 {
		pageSize = 20
	}

	values := url.Values{}
	values.Set("q", query)
	values.Set("mode", mode)
	values.Set("page_size", strconv.Itoa(pageSize))
	if explain {
		values.Set("explain", "1")
	}

	var payload SearchResponse
	if err := c.getJSON(ctx, "/api/v1/search?"+values.Encode(), &payload); err != nil {
		return SearchResponse{}, err
	}
	return payload, nil
}

func (c *Client) SearchIDs(ctx context.Context, query, mode string, pageSize int) ([]int64, error) {
	response, err := c.Search(ctx, query, mode, pageSize, false)
	if err != nil {
		return nil, err
	}
	items := response.Items()
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

func (c *Client) Message(ctx context.Context, id int64) (Message, error) {
	if id <= 0 {
		return Message{}, fmt.Errorf("message id is required")
	}
	var payload Message
	if err := c.getJSON(ctx, "/api/v1/messages/"+strconv.FormatInt(id, 10), &payload); err != nil {
		return Message{}, err
	}
	return payload, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	if c.baseURL == "" {
		return fmt.Errorf("msgvault API URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if header := c.authHeader(); header != "" {
		req.Header.Set("Authorization", header)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("msgvault API %s returned %d: %s", path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode msgvault API %s: %w", path, err)
	}
	return nil
}

func (c *Client) authHeader() string {
	if c.apiKey == "" {
		return ""
	}
	return "Bearer " + c.apiKey
}

func normalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "vector", "hybrid", "fts":
		return mode
	default:
		return "fts"
	}
}

// RequiresFTSMode returns true when a query uses msgvault/Gmail-style search
// syntax or punctuation that the HTTP API's hybrid BM25 leg currently sends
// through raw FTS5. Those queries are valid in mode=fts, but mode=hybrid can
// fail before Memento's fallback gets a chance to recover.
func RequiresFTSMode(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	lower := strings.ToLower(query)
	for _, op := range []string{
		"from:", "to:", "cc:", "bcc:", "subject:", "label:", "l:",
		"has:", "before:", "after:", "older_than:", "newer_than:",
		"larger:", "smaller:",
	} {
		if strings.Contains(lower, op) {
			return true
		}
	}
	if strings.Contains(query, "\"") || strings.Contains(query, "@") {
		return true
	}
	for _, token := range strings.Fields(query) {
		upper := strings.ToUpper(token)
		if upper == "OR" || upper == "AND" || upper == "NOT" || upper == "NEAR" {
			return true
		}
		if strings.ContainsAny(token, ".-/") {
			return true
		}
	}
	return false
}
