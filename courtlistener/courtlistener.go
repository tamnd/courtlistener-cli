// Package courtlistener is the library behind the courtlistener command line:
// the HTTP client, request shaping, and the typed data models for US court
// opinions fetched from the CourtListener REST API.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package courtlistener

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Host is the site this client talks to, and the host the URI driver in
// domain.go claims.
const Host = "www.courtlistener.com"

// BaseURL is the root every request is built from.
const BaseURL = "https://www.courtlistener.com/api/rest/v4"

// Config holds all tuneable parameters for the Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults for the CourtListener API.
// CourtListener can rate-limit aggressive scrapers, so we default to 500ms
// between requests and a generous 15s timeout.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: "tamnd-courtlistener-cli/0.1 (tamnd87@gmail.com)",
		Rate:      500 * time.Millisecond,
		Retries:   3,
		Timeout:   15 * time.Second,
	}
}

// Client talks to the CourtListener REST API over HTTPS.
type Client struct {
	HTTP      *http.Client
	cfg       Config
	last      time.Time
}

// NewClient returns a Client using DefaultConfig.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP: &http.Client{Timeout: cfg.Timeout},
		cfg:  cfg,
	}
}

// NewClientWithConfig returns a Client using the provided Config.
func NewClientWithConfig(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = BaseURL
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &Client{
		HTTP: &http.Client{Timeout: cfg.Timeout},
		cfg:  cfg,
	}
}

// Opinion is a single court opinion record.
type Opinion struct {
	ID        int      `json:"id" kit:"id"`
	CaseName  string   `json:"case_name"`
	Court     string   `json:"court"`
	DateFiled string   `json:"date_filed"`
	Status    string   `json:"status"`
	Citation  []string `json:"citation"`
	Judge     string   `json:"judge"`
	Snippet   string   `json:"snippet"`
	URL       string   `json:"url"`
}

// wire types — match the CourtListener search API JSON exactly.

type wireSearch struct {
	Count   int           `json:"count"`
	Results []wireOpinion `json:"results"`
}

type wireOpinion struct {
	ClusterID   int      `json:"cluster_id"`
	CaseName    string   `json:"caseName"`
	CourtID     string   `json:"court_id"`
	DateFiled   string   `json:"dateFiled"`
	Status      string   `json:"status"`
	Citation    []string `json:"citation"`
	Judge       string   `json:"judge"`
	Snippet     string   `json:"snippet"`
	SuitNature  string   `json:"suitNature"`
	AbsoluteURL string   `json:"absolute_url"`
}

func (w wireOpinion) toOpinion() Opinion {
	absURL := w.AbsoluteURL
	if absURL != "" && absURL[0] == '/' {
		absURL = "https://" + Host + absURL
	}
	return Opinion{
		ID:        w.ClusterID,
		CaseName:  w.CaseName,
		Court:     w.CourtID,
		DateFiled: w.DateFiled,
		Status:    w.Status,
		Citation:  w.Citation,
		Judge:     w.Judge,
		Snippet:   w.Snippet,
		URL:       absURL,
	}
}

// SearchOpinions searches the CourtListener opinion index.
//
// query is the full-text search string. court optionally filters by court ID
// (e.g. "scotus", "ca9", "nysd"). searchType selects the result type: "o" for
// opinions (default), "oa" for oral arguments, "r" for RECAP. limit caps the
// number of returned results (0 means no explicit limit passed to the API).
func (c *Client) SearchOpinions(ctx context.Context, query, court, searchType string, limit int) ([]Opinion, error) {
	if searchType == "" {
		searchType = "o"
	}
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("type", searchType)
	params.Set("stat_Precedential", "on")
	params.Set("limit", strconv.Itoa(limit))
	if court != "" {
		params.Set("court", court)
	}

	endpoint := c.cfg.BaseURL + "/search/?" + params.Encode()
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var ws wireSearch
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("courtlistener: parse search response: %w", err)
	}

	out := make([]Opinion, 0, len(ws.Results))
	for _, w := range ws.Results {
		out = append(out, w.toOpinion())
	}
	return out, nil
}

// get fetches url with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
