package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAPIErrorParsesObjectEnvelope(t *testing.T) {
	body := []byte(`{"error":{"code":"FREE_ANALYSIS_USED","message":"Daily analysis limit reached. Upgrade for 50 analyses a day."}}`)
	err := newAPIError(http.StatusTooManyRequests, http.Header{}, body)

	if err.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", err.Status)
	}
	if err.Code != "FREE_ANALYSIS_USED" {
		t.Errorf("Code = %q, want FREE_ANALYSIS_USED", err.Code)
	}
	if err.Message != "Daily analysis limit reached. Upgrade for 50 analyses a day." {
		t.Errorf("Message = %q", err.Message)
	}
	if got := err.Error(); !strings.Contains(got, "FREE_ANALYSIS_USED") {
		t.Errorf("Error() = %q, want the code in it", got)
	}
}

func TestAPIErrorParsesValidationField(t *testing.T) {
	body := []byte(`{"error":{"code":"VALIDATION_ERROR","message":"A valid email is required to run a free audit","details":{"field":"email"}}}`)
	err := newAPIError(http.StatusBadRequest, http.Header{}, body)

	if err.Code != "VALIDATION_ERROR" || err.Field != "email" {
		t.Errorf("Code = %q, Field = %q, want VALIDATION_ERROR and email", err.Code, err.Field)
	}
}

func TestAPIErrorParsesStringEnvelope(t *testing.T) {
	// The hook tool's anonymous quota answer, the only place the API puts a
	// bare string where the object envelope normally goes.
	body := []byte(`{"error":"email_required","message":"Free anonymous hook analysis used for today."}`)
	err := newAPIError(http.StatusPaymentRequired, http.Header{}, body)

	if err.Code != "email_required" {
		t.Errorf("Code = %q, want email_required", err.Code)
	}
	if err.Message != "Free anonymous hook analysis used for today." {
		t.Errorf("Message = %q", err.Message)
	}
	if err.Status != http.StatusPaymentRequired {
		t.Errorf("Status = %d, want 402", err.Status)
	}
}

func TestAPIErrorParsesNumericCode(t *testing.T) {
	// The body-size middleware answers with a number where every other route
	// puts a code string.
	body := []byte(`{"error":{"code":413,"message":"Request body too large. Maximum size is 250 MB."}}`)
	err := newAPIError(http.StatusRequestEntityTooLarge, http.Header{}, body)

	if err.Code != "413" {
		t.Errorf("Code = %q, want \"413\"", err.Code)
	}
	if err.Message != "Request body too large. Maximum size is 250 MB." {
		t.Errorf("Message = %q", err.Message)
	}
}

func TestAPIErrorSurvivesNonJSONBody(t *testing.T) {
	// A proxy or a load balancer answering with an HTML error page.
	body := []byte("<html><body>502 Bad Gateway</body></html>")
	err := newAPIError(http.StatusBadGateway, http.Header{}, body)

	if err.Code != "" || err.Message != "" {
		t.Errorf("Code = %q, Message = %q, want both empty", err.Code, err.Message)
	}
	if got := err.Error(); got != "HTTP 502" {
		t.Errorf("Error() = %q, want a status fallback", got)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "30")
	err := newAPIError(http.StatusTooManyRequests, header, []byte(`{"error":{"code":"RATE_LIMITED","message":"Too many requests."}}`))
	if err.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", err.RetryAfter)
	}

	// The HTTP-date form is also legal, and a date in the past means "now".
	header.Set("Retry-After", time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat))
	err = newAPIError(http.StatusTooManyRequests, header, nil)
	if err.RetryAfter < 90*time.Second || err.RetryAfter > 2*time.Minute {
		t.Errorf("RetryAfter = %v, want roughly 2 minutes", err.RetryAfter)
	}

	header.Set("Retry-After", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))
	if got := newAPIError(http.StatusTooManyRequests, header, nil).RetryAfter; got != 0 {
		t.Errorf("RetryAfter for a past date = %v, want 0", got)
	}

	if got := newAPIError(http.StatusTooManyRequests, http.Header{}, nil).RetryAfter; got != 0 {
		t.Errorf("RetryAfter with no header = %v, want 0", got)
	}
}

func TestIsCodeAndStatusOf(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &APIError{Status: 404, Code: "NOT_FOUND", Message: "Analysis not found"})

	if !IsCode(err, "NOT_FOUND") {
		t.Error("IsCode missed a wrapped APIError")
	}
	if !IsCode(err, "not_found") {
		t.Error("IsCode should compare codes case-insensitively")
	}
	if IsCode(err, "RATE_LIMITED") {
		t.Error("IsCode matched the wrong code")
	}
	if got := StatusOf(err); got != 404 {
		t.Errorf("StatusOf = %d, want 404", got)
	}
	if StatusOf(errors.New("plain")) != 0 {
		t.Error("StatusOf on a non-APIError should be 0")
	}
	if IsCode(errors.New("plain"), "NOT_FOUND") {
		t.Error("IsCode on a non-APIError should be false")
	}
}
