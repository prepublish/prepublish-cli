// Package api is the Prepublish HTTP client: transport, retry policy, wire
// types and typed errors. It knows nothing about terminals, prompts or cobra;
// every method takes a context and returns either a decoded response or an
// *APIError.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/version"
)

// maxResponseBytes caps how much of a response body is read. The largest real
// body is a full report, well under a megabyte; the cap exists so a runaway or
// hostile response cannot exhaust memory.
const maxResponseBytes = 8 << 20

// requestTimeout is the deadline for one attempt. It is generous because a
// cold free tool run can take half a minute server-side, and the contexts the
// CLI passes in (and Ctrl-C) are the real cancellation mechanism.
const requestTimeout = 60 * time.Second

// retryBackoff is the wait before each retry, so a request is attempted
// len(retryBackoff)+1 times at most. Roughly 7 seconds of patience: enough to
// cover an API container restart, short enough that a person notices a failure
// instead of wondering whether the command is stuck.
var retryBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// Client is an API client. It is safe for concurrent use; methods do not
// mutate it after New.
type Client struct {
	baseURL string
	token   string
	ua      string
	http    *http.Client
}

// New builds a client for baseURL. An empty baseURL falls back to
// config.DefaultAPIURL and an empty token means every call is anonymous, which
// is a supported mode rather than an error: the free tier has no credential.
func New(baseURL, token, userAgent string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = config.DefaultAPIURL
	}
	if userAgent == "" {
		userAgent = version.UserAgent()
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(token),
		ua:      userAgent,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// BaseURL is the API root this client talks to, for error messages and for
// `--api-url` diagnostics.
func (c *Client) BaseURL() string { return c.baseURL }

// Authenticated reports whether calls carry a bearer.
func (c *Client) Authenticated() bool { return c.token != "" }

// do sends one request and returns the raw body of a 2xx, or an *APIError.
//
// Retry policy, copied from the MCP bridge because the reasoning is the same:
//
//   - a GET is repeated after any transport failure and after 502/503/504. It
//     has no side effect to repeat, and every deploy recreates the API
//     container, so a read landing in that window should not surface as a
//     failure.
//   - a request with a body is repeated only when the connection was refused or
//     the host did not resolve. A timeout or a reset may mean the server is
//     already working on the request — repeating it could run a second audit —
//     so those failures are reported instead.
//   - a 4xx is never repeated. It is the server's answer, and 429 already
//     carries Retry-After for the caller to act on.
func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	var lastErr *APIError

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build %s %s: %w", method, path, err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.ua)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// A cancelled or expired context is the caller's answer, not a
			// network failure to retry or to dress up.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = &APIError{
				Status:  0,
				Code:    CodeNetworkError,
				Message: "could not reach the Prepublish API at " + c.baseURL + ": " + err.Error(),
				Cause:   err,
			}
			if attempt < len(retryBackoff) && retryableTransport(method, err) {
				if err := sleepCtx(ctx, retryBackoff[attempt]); err != nil {
					return nil, err
				}
				continue
			}
			return nil, lastErr
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = &APIError{
				Status:  0,
				Code:    CodeNetworkError,
				Message: "reading the response from " + c.baseURL + ": " + readErr.Error(),
				Cause:   readErr,
			}
			if attempt < len(retryBackoff) && retryableTransport(method, readErr) {
				if err := sleepCtx(ctx, retryBackoff[attempt]); err != nil {
					return nil, err
				}
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return raw, nil
		}

		apiErr := newAPIError(resp.StatusCode, resp.Header, raw)
		if method == http.MethodGet && attempt < len(retryBackoff) && transientStatus(resp.StatusCode) {
			if err := sleepCtx(ctx, retryBackoff[attempt]); err != nil {
				return nil, err
			}
			continue
		}
		return nil, apiErr
	}
}

// retryableTransport reports whether a transport failure proves the request
// never reached the server.
func retryableTransport(method string, err error) bool {
	if method == http.MethodGet {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	// A GET is repeated even when the request may have been processed, so
	// nothing here needs to distinguish "refused" from "reset" for it.
	if errors.Is(err, syscall.ECONNRESET) && method == http.MethodGet {
		return true
	}
	return false
}

// transientStatus is the set of answers that mean "this server is not serving
// right now": a deployed or overloaded API, not a rejected request.
func transientStatus(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// sleepCtx waits for d, or returns early with the context's error.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// getJSON performs a GET and decodes a 2xx body into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	raw, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	return decode(raw, path, out)
}

// postJSON performs a JSON POST and decodes a 2xx body into out.
func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", path, err)
	}
	raw, err := c.do(ctx, http.MethodPost, path, payload, "application/json")
	if err != nil {
		return err
	}
	return decode(raw, path, out)
}

// delete performs a DELETE and discards a 2xx body.
func (c *Client) delete(ctx context.Context, path string) error {
	_, err := c.do(ctx, http.MethodDelete, path, nil, "")
	return err
}

// decode unmarshals a body into out. An empty body is not an error: 204 and a
// success response that carries no content are both real.
func decode(raw []byte, path string, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &APIError{
			Status:  http.StatusOK,
			Code:    CodeUnexpectedResponse,
			Message: fmt.Sprintf("could not read the response from %s: %v", path, err),
		}
	}
	return nil
}

// withQuery appends encoded query parameters to path, skipping empty values so
// an absent filter stays absent instead of becoming `?page=`.
func withQuery(path string, params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
