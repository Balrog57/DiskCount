package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxBodyBytes = 5 << 20

type HTTPFetcher struct {
	Client  *http.Client
	Headers map[string]string
}

func NewHTTPFetcher(userAgent string, timeoutSeconds float64) *HTTPFetcher {
	return &HTTPFetcher{
		Client: &http.Client{
			Timeout: time.Duration(timeoutSeconds * float64(time.Second)),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		Headers: map[string]string{
			"User-Agent":      userAgent,
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
		},
	}
}

func (f *HTTPFetcher) Get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading body: %w", err)
	}

	return string(body), nil
}

func (f *HTTPFetcher) PostJSON(ctx context.Context, url string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading body: %w", err)
	}

	return string(respBody), nil
}
