package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error codes the client and the UI branch on. They are the backend's own
// strings; the list here is only the ones a CLI has something better to say
// about than "request failed".
const (
	// CodeNetworkError is ours, not the server's: it means no HTTP status
	// exists because the API was never reached.
	CodeNetworkError = "NETWORK_ERROR"
	// CodeUnexpectedResponse is ours as well: a 2xx whose body was not the
	// shape the endpoint documents.
	CodeUnexpectedResponse = "UNEXPECTED_RESPONSE"
)

// Error codes the server sends. They are constants rather than literals at the
// call site because a mistyped code fails silently: IsCode simply returns
// false and the error falls through to the generic branch.
const (
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"
	// CodeValidation carries a field name in APIError.Field.
	CodeValidation = "VALIDATION_ERROR"
	// CodeRateLimited arrives with Retry-After when the limiter is per
	// endpoint; the global limiter sends no header.
	CodeRateLimited = "RATE_LIMITED"
	// CodeFreeAnalysisUsed is the daily audit cap, for both the anonymous and
	// the account allowance.
	CodeFreeAnalysisUsed = "FREE_ANALYSIS_USED"
	// CodeNoSubscription is the analyze route refusing a file or thumbnail for
	// a caller who is not paid.
	CodeNoSubscription = "NO_SUBSCRIPTION"
	// CodeSubscriptionRequired is a RequireSubscription route (uploads) turning
	// away a free account.
	CodeSubscriptionRequired = "SUBSCRIPTION_REQUIRED"
	// CodeLimitExceeded is the ten-live-keys cap on the account.
	CodeLimitExceeded = "LIMIT_EXCEEDED"
	// CodeEmailRequired is the hook tool's anonymous quota answer. It is
	// lowercase because it arrives in the bare-string envelope, not the
	// standard one.
	CodeEmailRequired = "email_required"

	// Device-flow codes. They are answers to a poll, not failures.
	CodeAuthorizationPending = "AUTHORIZATION_PENDING"
	CodeAccessDenied         = "ACCESS_DENIED"
	CodeExpiredToken         = "EXPIRED_TOKEN"
	// CodeInvalidGrant is an unknown or already consumed device code, which
	// means the request has to be started again.
	CodeInvalidGrant = "INVALID_GRANT"
)

// APIError is any failure the server answered with, plus the two failures that
// never reached it. It is the only error type this package returns, so a caller
// can branch on a code instead of string-matching a message.
type APIError struct {
	// Status is the HTTP status, or 0 when the API was unreachable.
	Status int
	// Code is the machine-readable code from the error envelope. The backend
	// sends a string ("FREE_ANALYSIS_USED", "RATE_LIMITED") except for the
	// body-size middleware, which sends a number; both arrive here as text.
	Code string
	// Message is the human-readable message, ready to print.
	Message string
	// Field is details.field when the error was a validation failure, so the
	// CLI can point at the flag that caused it (`--email`, a file path).
	Field string
	// RetryAfter is the parsed Retry-After header. Zero when absent, which is
	// the common case for the global limiter.
	RetryAfter time.Duration
	// RawBody is the untouched response, kept for the 409 upload body (whose
	// resume offset sits outside the error envelope) and for bug reports.
	RawBody []byte
	// Cause is the transport failure behind Status 0. It keeps
	// errors.Is(err, context.DeadlineExceeded) working for callers that care
	// why the connection did not happen.
	Cause error
}

// Error implements error.
func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return e.Code + ": " + e.Message
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	default:
		return fmt.Sprintf("HTTP %d", e.Status)
	}
}

// Unwrap exposes the transport cause, if any.
func (e *APIError) Unwrap() error { return e.Cause }

// IsCode reports whether err is an APIError with the given code. The comparison
// is case-insensitive because the code is compared against a constant in the
// caller while the server owns the spelling.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return strings.EqualFold(apiErr.Code, code)
}

// StatusOf returns the HTTP status behind err, or 0 when err is not an
// APIError or the API was unreachable.
func StatusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// RetryAfterOf returns the server's requested wait behind err, or 0 when there
// is none. It is the Retry-After header of a rate limit, which is the one place
// the API says when to come back rather than only saying no.
func RetryAfterOf(err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.RetryAfter
	}
	return 0
}

// newAPIError builds an APIError from a non-2xx response.
//
// Two envelopes are in production and both must parse. The standard one wraps
// the code in an object (`{"error":{"code","message","details"}}`); the free
// hook tool answers a quota-exhausted anonymous caller with a bare string
// (`{"error":"email_required","message":"…"}`). A body that is not JSON at all
// (an HTML page from a proxy or a load balancer) leaves the code empty, and
// Error() falls back to the status rather than printing raw markup.
func newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{
		Status:     status,
		RawBody:    body,
		RetryAfter: parseRetryAfter(header.Get("Retry-After")),
	}

	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return e
	}
	e.Message = envelope.Message

	raw := bytes.TrimSpace(envelope.Error)
	if len(raw) == 0 {
		return e
	}

	if raw[0] == '{' {
		var detail struct {
			Code    json.RawMessage            `json:"code"`
			Message string                     `json:"message"`
			Details map[string]json.RawMessage `json:"details"`
		}
		if err := json.Unmarshal(raw, &detail); err != nil {
			return e
		}
		e.Code = scalarString(detail.Code)
		if detail.Message != "" {
			e.Message = detail.Message
		}
		e.Field = scalarString(detail.Details["field"])
		return e
	}

	e.Code = scalarString(raw)
	return e
}

// scalarString renders a JSON string or number as text and anything else as "".
// The body-size middleware answers with `{"error":{"code":413}}`, so a numeric
// code is a real case, not defensive padding.
func scalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// parseRetryAfter understands both forms RFC 9110 allows: a delay in seconds
// and an HTTP date.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
