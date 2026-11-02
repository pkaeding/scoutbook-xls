package scouting

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrTokenExpired is returned when the Scouting API responds with 401,
// indicating the JWT has expired and the user must supply a fresh one.
var ErrTokenExpired = errors.New("scouting: token expired")

// Client is a thin HTTP client for the unofficial advancements.scouting.org API.
type Client struct {
	baseURL        string
	token          string
	httpClient     *http.Client
	maxRetries     int
	retryBaseDelay time.Duration
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient overrides the underlying *http.Client.
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = h
	}
}

// WithMaxRetries sets the total number of attempts the client will make for
// a single Get call (including the first). Must be >= 1; values <= 0 fall back
// to the default.
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) {
		if n > 0 {
			c.maxRetries = n
		}
	}
}

// WithRetryBaseDelay sets the base delay used in exponential backoff between
// retries. Actual delay is baseDelay * 2^attempt.
func WithRetryBaseDelay(d time.Duration) ClientOption {
	return func(c *Client) {
		c.retryBaseDelay = d
	}
}

// NewClient constructs a Client that talks to baseURL with the given bearer token.
func NewClient(baseURL, token string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:        baseURL,
		token:          token,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		maxRetries:     3,
		retryBaseDelay: 500 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get performs a GET request against path (appended to baseURL), decoding the
// JSON response body into out. esbTarget is the URL to base64-encode into the
// x-esb-url header. Transient failures (429, 5xx, network errors) are retried.
func (c *Client) Get(ctx context.Context, path, esbTarget string, out any) error {
	return c.do(ctx, http.MethodGet, path, esbTarget, nil, out, true)
}

// Post performs a POST request with body JSON-encoded, decoding the JSON
// response into out (which may be nil to discard the response).
//
// Unlike Get, only a 429 (the server explicitly declined to process the
// request) is retried: a 5xx or a dropped connection leaves it unknown
// whether a write was applied, and the caller is better placed to decide
// whether replaying it is safe. Non-2xx errors include the start of the
// response body, since write failures are otherwise hard to diagnose.
func (c *Client) Post(ctx context.Context, path, esbTarget string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, esbTarget, body, out, false)
}

// do is the shared request loop behind Get and Post. retryTransient controls
// whether 5xx responses and network errors are retried; 429 always is.
func (c *Client) do(ctx context.Context, method, path, esbTarget string, body, out any, retryTransient bool) error {
	url := c.baseURL + path
	esbEncoded := base64.StdEncoding.EncodeToString([]byte(esbTarget))

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("scouting: encode request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryBaseDelay << attempt
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("x-esb-url", esbEncoded)
		req.Header.Set("Origin", "https://advancements.scouting.org")
		req.Header.Set("Referer", "https://advancements.scouting.org/")
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			lastErr = err
			if !retryTransient {
				return err
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer func() { _ = resp.Body.Close() }()
			if out == nil {
				return nil
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("scouting: decode response: %w", err)
			}
			return nil
		}

		if resp.StatusCode == http.StatusUnauthorized {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return fmt.Errorf("scouting: 401 unauthorized: %w", ErrTokenExpired)
		}

		if resp.StatusCode == http.StatusTooManyRequests || (retryTransient && resp.StatusCode >= 500) {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("scouting: retryable status %d", resp.StatusCode)
			continue
		}

		// Non-retryable, non-401 error.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if method == http.MethodGet || len(snippet) == 0 {
			return fmt.Errorf("scouting: unexpected status %d", resp.StatusCode)
		}
		return fmt.Errorf("scouting: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	if lastErr == nil {
		lastErr = errors.New("scouting: retries exhausted")
	}
	return lastErr
}
