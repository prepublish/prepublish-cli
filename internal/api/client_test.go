package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetry shrinks the backoff for the duration of a test so the retry policy
// can be exercised without spending the seconds it is designed to spend in
// production. It restores the real delays on cleanup.
func fastRetry(t *testing.T) {
	t.Helper()
	original := retryBackoff
	retryBackoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
	t.Cleanup(func() { retryBackoff = original })
}

func TestGetRetriesTransientStatuses(t *testing.T) {
	fastRetry(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, `{"error":{"code":"INTERNAL_ERROR","message":"An internal error occurred."}}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tier":"free_account"}`)
	}))
	defer srv.Close()

	usage, err := New(srv.URL, "pp_live_test", "test-agent").Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Tier != "free_account" {
		t.Errorf("Tier = %q, want free_account", usage.Tier)
	}
	// A GET has nothing to repeat, and a deploy that recreates the API
	// container must not turn into a user-visible failure.
	if calls != 3 {
		t.Errorf("server saw %d calls, want 3 (two 503s then success)", calls)
	}
}

func TestGetDoesNotRetryClientErrors(t *testing.T) {
	fastRetry(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"code":"RATE_LIMITED","message":"Too many requests. Please wait and try again."}}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "", "").Usage(context.Background())
	if !IsCode(err, "RATE_LIMITED") {
		t.Fatalf("err = %v, want RATE_LIMITED", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter = %v, want the header's 12s surfaced to the caller", apiErr.RetryAfter)
	}
	if calls != 1 {
		t.Errorf("server saw %d calls, want 1: a 4xx is the server's answer, not a retry", calls)
	}
}

func TestPostDoesNotRetryServerErrors(t *testing.T) {
	fastRetry(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"error":{"code":"INTERNAL_ERROR","message":"An internal error occurred."}}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// A POST may have already started work server-side, so an overloaded API
	// must surface rather than silently double-submit.
	_, err := New(srv.URL, "", "").HookAnalyze(context.Background(), "script", "", "")
	if StatusOf(err) != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want a 503", err)
	}
	if calls != 1 {
		t.Errorf("server saw %d calls, want 1", calls)
	}
}

func TestPostRetriesRefusedConnection(t *testing.T) {
	fastRetry(t)

	// A closed listener gives a real ECONNREFUSED: proof the request never
	// arrived, which is the one transport failure a POST may repeat.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	start := time.Now()
	_, err := New(url, "", "").HookAnalyze(context.Background(), "script", "", "")
	if !IsCode(err, CodeNetworkError) {
		t.Fatalf("err = %v, want NETWORK_ERROR", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("returned after %v, want the three backoff waits to have happened", elapsed)
	}
}

func TestAnalyzeJSONOmitsUnsetFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer pp_live_test" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "test-agent/1" {
			t.Errorf("User-Agent = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"id":"a-1","status":"pending","progress":0,"retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	a, err := New(srv.URL, "pp_live_test", "test-agent/1").Analyze(context.Background(), AnalyzeRequest{
		VideoTitle:      "How to ship a CLI",
		ScriptText:      strings.Repeat("word ", 60),
		AnonymousUserID: "cli:abc",
	}, "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if a.ID != "a-1" || a.Status != StatusPending {
		t.Errorf("analysis = %+v, want id a-1 and status pending", a)
	}

	if _, ok := body["video_duration"]; ok {
		t.Error("video_duration was sent; unknown must stay absent rather than become zero")
	}
	if _, ok := body["email"]; ok {
		t.Error("empty email was sent; an empty address is not the same as no address")
	}
	if body["anonymous_user_id"] != "cli:abc" {
		t.Errorf("anonymous_user_id = %v", body["anonymous_user_id"])
	}
}

func TestAnalyzeMultipartWithThumbnail(t *testing.T) {
	dir := t.TempDir()
	thumb := filepath.Join(dir, "cover.png")
	if err := os.WriteFile(thumb, []byte("fake-png-bytes"), 0o600); err != nil {
		t.Fatalf("write thumbnail: %v", err)
	}

	var (
		fields   map[string]string
		fileName string
		fileBody string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("Content-Type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		fields = map[string]string{}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read part: %v", err)
			}
			data, _ := io.ReadAll(part)
			if part.FileName() != "" {
				fileName = part.FileName()
				fileBody = string(data)
				continue
			}
			fields[part.FormName()] = string(data)
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"id":"a-2","status":"pending"}`)
	}))
	defer srv.Close()

	duration := 615
	_, err := New(srv.URL, "", "").Analyze(context.Background(), AnalyzeRequest{
		VideoTitle:    "With a thumbnail",
		ScriptText:    strings.Repeat("word ", 60),
		VideoDuration: &duration,
		Email:         "creator@example.com",
		Category:      "education",
	}, thumb)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if fields["video_title"] != "With a thumbnail" || fields["category"] != "education" {
		t.Errorf("form fields = %v", fields)
	}
	if fields["video_duration"] != "615" {
		t.Errorf("video_duration = %q, want the pointer's value as a string", fields["video_duration"])
	}
	if fileName != "cover.png" || fileBody != "fake-png-bytes" {
		t.Errorf("thumbnail part = %q / %q", fileName, fileBody)
	}
	// The API rejects a general `file` part by name; an upload has to go
	// through POST /api/uploads first.
	if _, ok := fields["file"]; ok {
		t.Error("a `file` field was sent, which the API rejects")
	}
}

func TestListAnalysesClampsPerPage(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		io.WriteString(w, `{"analyses":null,"total":0,"page":2,"per_page":50,"total_pages":1}`)
	}))
	defer srv.Close()

	page, err := New(srv.URL, "pp_live_test", "").ListAnalyses(context.Background(), 2, 500)
	if err != nil {
		t.Fatalf("ListAnalyses: %v", err)
	}
	if !strings.Contains(query, "per_page=50") || !strings.Contains(query, "page=2") {
		t.Errorf("query = %q, want the clamped per_page and the page", query)
	}
	if page.PerPage != 50 {
		t.Errorf("PerPage = %d", page.PerPage)
	}
}

func TestCheckFreeSendsAnonymousIDOnlyWhenSet(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		io.WriteString(w, `{"can_use_free":true,"remaining":2,"limit":3,"tier":"anonymous","max_file_size_mb":500}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "")
	if _, err := c.CheckFree(context.Background(), "cli:abc"); err != nil {
		t.Fatalf("CheckFree: %v", err)
	}
	if _, err := c.CheckFree(context.Background(), ""); err != nil {
		t.Fatalf("CheckFree (anonymous): %v", err)
	}

	if queries[0] != "anonymous_user_id=cli%3Aabc" {
		t.Errorf("query = %q, want the anonymous id", queries[0])
	}
	if queries[1] != "" {
		t.Errorf("query = %q, want no filter when there is no anonymous id", queries[1])
	}
}

func TestRevokeCurrentKeyUsesCurrentEndpoint(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := New(srv.URL, "pp_live_test", "").RevokeCurrentKey(context.Background()); err != nil {
		t.Fatalf("RevokeCurrentKey: %v", err)
	}
	if method != http.MethodDelete || path != "/api/user/api-keys/current" {
		t.Errorf("request = %s %s", method, path)
	}
}

func TestPostUsesNoTokenWhenAnonymous(t *testing.T) {
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		io.WriteString(w, `{"evaluation_id":"e-1","sentences":[],"top_issues":[],"rewrites":[]}`)
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "", "").HookAnalyze(context.Background(), "hook", "", ""); err != nil {
		t.Fatalf("HookAnalyze: %v", err)
	}
	if got, _ := auth.Load().(string); got != "" {
		t.Errorf("Authorization = %q, want no header for an anonymous call", got)
	}
}
