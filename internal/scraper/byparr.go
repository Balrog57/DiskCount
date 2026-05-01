package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type ByparrClient struct {
	BaseURL    string
	HTTPClient *http.Client
	mu         sync.Mutex
}

type ByparrSession struct {
	Cookies    []map[string]interface{} `json:"cookies"`
	UserAgent  string                   `json:"userAgent"`
	HTML       string                   `json:"body"`
	StatusCode int                      `json:"status"`
}

type byparrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type byparrResponse struct {
	Solution byparrSessionRaw `json:"solution"`
	Status   string           `json:"status"`
	Message  string           `json:"message"`
}

type byparrSessionRaw struct {
	Cookies    []map[string]interface{} `json:"cookies"`
	UserAgent  string                   `json:"userAgent"`
	Body       string                   `json:"body"`
	Status     int                      `json:"status"`
}

func NewByparrClient(baseURL string) *ByparrClient {
	return &ByparrClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{},
	}
}

func (c *ByparrClient) GetPage(ctx context.Context, targetURL string) (*ByparrSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	payload := byparrRequest{
		Cmd:        "request.get",
		URL:        targetURL,
		MaxTimeout: 60000,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.BaseURL+"/v1", bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result byparrResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("byparr error: %s", result.Message)
	}

	return &ByparrSession{
		Cookies:    result.Solution.Cookies,
		UserAgent:  result.Solution.UserAgent,
		HTML:       result.Solution.Body,
		StatusCode: result.Solution.Status,
	}, nil
}

func (c *ByparrClient) GetCookies(ctx context.Context, targetURL string) (map[string]string, string, error) {
	session, err := c.GetPage(ctx, targetURL)
	if err != nil {
		return nil, "", fmt.Errorf("getting page: %w", err)
	}

	cookies := make(map[string]string)
	for _, ck := range session.Cookies {
		if name, ok := ck["name"].(string); ok {
			if value, ok := ck["value"].(string); ok {
				cookies[name] = value
			}
		}
	}

	return cookies, session.UserAgent, nil
}
