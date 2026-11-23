package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bmd/internal/model"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client fetches paginated notice-board JSON.
type Client struct {
	BaseURL    string
	Cookie     string
	HTTP       *http.Client
	PageSize   int
	Referer    string
	RetryCount int
	RetryDelay time.Duration
}

// New creates a client with browser-like defaults.
func New(baseURL string, timeout time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://oag.ecitizen.go.ke"
	}
	return &Client{
		BaseURL:    baseURL,
		HTTP:       &http.Client{Timeout: timeout},
		PageSize:   30,
		Referer:    "https://oag.ecitizen.go.ke/notice-board",
		RetryCount: 4,
		RetryDelay: 2 * time.Second,
	}
}

// FetchPage fetches one page of a dataset ("marriages" or "impediments").
func (c *Client) FetchPage(ctx context.Context, dataset string, page, pageSize int) (model.PageResponse, error) {
	var out model.PageResponse
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = c.PageSize
		if pageSize < 1 {
			pageSize = 30
		}
	}
	endpoint := fmt.Sprintf("%s/notice-board/data/%s?page=%d&page_size=%d",
		c.BaseURL, url.PathEscape(dataset), page, pageSize)

	var lastErr error
	for attempt := 0; attempt <= c.RetryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(c.RetryDelay * time.Duration(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Referer", c.Referer)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		if c.Cookie != "" {
			req.Header.Set("Cookie", c.Cookie)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("GET %s: %s (retryable)", endpoint, resp.Status)
			continue
		}
		if resp.StatusCode == http.StatusForbidden && isChallenge(body) {
			return out, fmt.Errorf("GET %s: blocked by Cloudflare challenge (HTTP 403). Open the notice board in a browser, copy your cookies (cf_clearance etc.) and pass via --cookie or BMD_COOKIE", endpoint)
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("GET %s: %s: %s", endpoint, resp.Status, truncate(body, 300))
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return out, fmt.Errorf("decode %s page %d: %w: %s", dataset, page, err, truncate(body, 300))
		}
		return out, nil
	}
	return out, fmt.Errorf("GET %s page %d failed after %d attempts: %v", dataset, page, c.RetryCount+1, lastErr)
}

// Download fetches raw bytes for a photo path like
// "/files/download?name=...&path=....jpeg", resolving it against BaseURL.
func (c *Client) Download(ctx context.Context, photoPath string) (data []byte, contentType string, err error) {
	u := photoPath
	if !strings.HasPrefix(u, "http") {
		if !strings.HasPrefix(u, "/") {
			u = "/" + u
		}
		u = c.BaseURL + u
	}
	var lastErr error
	for attempt := 0; attempt <= c.RetryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(c.RetryDelay * time.Duration(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "image/*,*/*")
		req.Header.Set("Referer", c.Referer)
		if c.Cookie != "" {
			req.Header.Set("Cookie", c.Cookie)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		ct := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("GET photo: %s (retryable)", resp.Status)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("GET photo: %s: %s", resp.Status, truncate(body, 200))
		}
		return body, ct, nil
	}
	return nil, "", fmt.Errorf("download photo failed after %d attempts: %v", c.RetryCount+1, lastErr)
}

func isChallenge(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "Just a moment") || strings.Contains(s, "_cf_chl_opt") || strings.Contains(s, "challenges.cloudflare.com")
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
