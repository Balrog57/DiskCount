package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// byparrSessionRaw maps the Byparr "solution" object. The HTML body field is
// accepted under two names because Byparr versions disagree:
//   - older / FlareSolverr-compatible: "body"
//   - current Byparr (2025+):          "response"
//
// Reading only "body" meant the headless fallback silently returned empty
// HTML on current Byparr, so diskprices' 403 (UA block) never recovered even
// though Byparr successfully fetched the page. The custom UnmarshalJSON picks
// whichever field is present, preferring "response" when both exist.
type byparrSessionRaw struct {
	Cookies   []map[string]interface{} `json:"cookies"`
	UserAgent string                   `json:"userAgent"`
	Body      string                   `json:"body"`
	Response  string                   `json:"response"`
	Status    int                      `json:"status"`
}

func (r *byparrSessionRaw) UnmarshalJSON(data []byte) error {
	// Avoid infinite recursion: define a local type that shares the fields
	// but has no methods, so json.Unmarshal populates it directly.
	type alias byparrSessionRaw
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = byparrSessionRaw(a)
	// Prefer "response" (current Byparr); fall back to "body" (FlareSolverr).
	if r.Response == "" && r.Body != "" {
		r.Response = r.Body
	}
	if r.Body == "" && r.Response != "" {
		r.Body = r.Response
	}
	return nil
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

	reqURL := c.BaseURL + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, NewPermanentError(reqURL, 0, "creating request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, classifyTransportError(reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, classifyHTTPStatus(reqURL, resp.StatusCode)
	}

	var result byparrResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, NewParseError(reqURL, "decoding byparr response", err)
	}

	if result.Status != "ok" {
		msg := result.Message
		if strings.Contains(strings.ToLower(msg), "timeout") {
			return nil, NewTransientError(reqURL, 0, fmt.Sprintf("byparr: %s", msg), nil)
		}
		return nil, NewPermanentError(reqURL, 0, fmt.Sprintf("byparr: %s", msg), nil)
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
