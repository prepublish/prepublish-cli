package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// loginServer stands in for the two CLI auth routes. polls holds what each poll
// answers in order; the last entry repeats.
type loginServer struct {
	mu     sync.Mutex
	polls  []pollAnswer
	seen   int
	starts int
	// startBody overrides the start response when set.
	startBody string
}

type pollAnswer struct {
	status int
	body   string
	// retryAfter sets the Retry-After header, which is how the endpoint limiter
	// says when to come back.
	retryAfter string
}

func (s *loginServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		switch r.URL.Path {
		case "/api/cli/auth/start":
			s.starts++
			if s.startBody != "" {
				w.WriteHeader(http.StatusCreated)
				io.WriteString(w, s.startBody)
				return
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"device_code":"dc-secret","user_code":"BCDF-GHJK","verification_uri":"https://prepublish.ai/cli/login","verification_uri_complete":"https://prepublish.ai/cli/login?code=BCDF-GHJK","expires_in":600,"interval":1}`)

		case "/api/cli/auth/token":
			answer := s.polls[len(s.polls)-1]
			if s.seen < len(s.polls) {
				answer = s.polls[s.seen]
			}
			s.seen++
			if answer.retryAfter != "" {
				w.Header().Set("Retry-After", answer.retryAfter)
			}
			if answer.status != 0 {
				w.WriteHeader(answer.status)
			}
			io.WriteString(w, answer.body)

		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"code":"NOT_FOUND","message":"The requested resource was not found."}}`)
		}
	})
}

func TestDeviceLoginWaitsForApproval(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusBadRequest, body: `{"error":{"code":"AUTHORIZATION_PENDING","message":"The user has not yet approved this request."}}`},
		{status: http.StatusOK, body: `{"api_key":"pp_live_issued","key_id":"key-9","key_prefix":"pp_live_issu","user":{"id":"u-1","email":"creator@example.com","subscription_status":"active","analysis_count":4,"created_at":"2026-01-01T00:00:00Z"}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	events := make(chan LoginEvent, 8)
	token, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", events)
	if err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}
	if token.APIKey != "pp_live_issued" || token.KeyID != "key-9" {
		t.Errorf("token = %+v", token)
	}
	if token.User.Email != "creator@example.com" {
		t.Errorf("user = %+v, want the identity the token carries", token.User)
	}

	close(events)
	var kinds []LoginEventKind
	var code string
	for ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Start != nil {
			code = ev.Start.UserCode
		}
	}
	// Started must carry the codes even on the event the UI sees first; the
	// polling events repeat them so a late reader is not left without them.
	if len(kinds) == 0 || kinds[0] != Started {
		t.Fatalf("events = %v, want the first to be Started", kinds)
	}
	if code != "BCDF-GHJK" {
		t.Errorf("user code = %q, want the code from the start response", code)
	}
	if len(kinds) != 2 || kinds[1] != Polling {
		t.Errorf("event kinds = %v, want one Started and one Polling", kinds)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.starts != 1 || srv.seen != 2 {
		t.Errorf("start calls = %d, polls = %d, want 1 and 2", srv.starts, srv.seen)
	}
}

func TestDeviceLoginDenied(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusForbidden, body: `{"error":{"code":"ACCESS_DENIED","message":"This request was denied."}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
}

func TestDeviceLoginExpired(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusGone, body: `{"error":{"code":"EXPIRED_TOKEN","message":"This login request has expired."}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestDeviceLoginStopsAtTheServersTTL(t *testing.T) {
	// A start response with a one-second lifetime and a poll that never leaves
	// AUTHORIZATION_PENDING: the client must stop on its own rather than poll
	// forever against a request the server has already thrown away.
	srv := &loginServer{
		startBody: `{"device_code":"dc-secret","user_code":"BCDF-GHJK","verification_uri":"u","verification_uri_complete":"u","expires_in":1,"interval":1}`,
		polls: []pollAnswer{
			{status: http.StatusBadRequest, body: `{"error":{"code":"AUTHORIZATION_PENDING","message":"waiting"}}`},
		},
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	start := time.Now()
	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("gave up after %v, want the advertised 1s TTL", elapsed)
	}
}

func TestDeviceLoginSurfacesUnexpectedAPIErrors(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusConflict, body: `{"error":{"code":"LIMIT_EXCEEDED","message":"You already have 10 active API keys."}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !api.IsCode(err, "LIMIT_EXCEEDED") {
		t.Fatalf("err = %v, want the API error passed through", err)
	}
}

func TestDeviceLoginSurfacesStartFailures(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"code":"RATE_LIMITED","message":"Too many requests. Please wait and try again."}}`)
	}))
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !api.IsCode(err, "RATE_LIMITED") {
		t.Fatalf("err = %v, want RATE_LIMITED", err)
	}
}

func TestDeviceLoginCancelledContext(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusBadRequest, body: `{"error":{"code":"AUTHORIZATION_PENDING","message":"waiting"}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel while the flow is waiting out the interval between polls.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := DeviceLogin(ctx, api.New(ts.URL, "", "test"), "test-host", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDeviceLoginRejectsIncompleteStartResponse(t *testing.T) {
	srv := &loginServer{startBody: `{"user_code":"BCDF-GHJK"}`}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if err == nil || errors.Is(err, ErrDenied) || errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want a protocol error, not a user outcome", err)
	}
}

func TestDeviceLoginSendsClientName(t *testing.T) {
	var body map[string]string
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusForbidden, body: `{"error":{"code":"ACCESS_DENIED","message":"denied"}}`},
	}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/auth/start" {
			json.NewDecoder(r.Body).Decode(&body)
		}
		srv.handler().ServeHTTP(w, r)
	}))
	defer ts.Close()

	if _, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "studio-mac", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if body["client_name"] != "studio-mac" {
		// The name becomes the key's label in the dashboard, which is how a
		// user with several machines tells them apart.
		t.Errorf("client_name = %q, want studio-mac", body["client_name"])
	}
}

// TestDeviceLoginWaitsOutARateLimit: the per-IP budget on the poll route is
// small, and the login takes as long as the user takes to open a mail, sign in
// and approve. A 429 there used to end the sign-in outright, which made anyone
// slower than about two minutes unable to log in at all.
func TestDeviceLoginWaitsOutARateLimit(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{
			status:     http.StatusTooManyRequests,
			retryAfter: "1",
			body:       `{"error":{"code":"RATE_LIMITED","message":"Too many requests. Please wait and try again."}}`,
		},
		{status: http.StatusOK, body: `{"api_key":"pp_live_after_429","key_id":"key-9","user":{"id":"u-1","email":"creator@example.com"}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	start := time.Now()
	token, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if err != nil {
		t.Fatalf("DeviceLogin gave up on a rate limit: %v", err)
	}
	if token.APIKey != "pp_live_after_429" {
		t.Errorf("token = %+v, want the key issued after the wait", token)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.seen != 2 {
		t.Errorf("polls = %d, want the flow to ask again after the rate limit", srv.seen)
	}
	// The wait is the server's own Retry-After (1s), not a guess: less than that
	// means the header was ignored, much more means it was padded out.
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("returned after %v, want it to have waited out the Retry-After", elapsed)
	}
}

// TestDeviceLoginBacksOffWithoutARetryAfterHeader covers the same case when the
// server only says "no": the interval grows, and the flow keeps asking.
func TestDeviceLoginBacksOffWithoutARetryAfterHeader(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusTooManyRequests, body: `{"error":{"code":"RATE_LIMITED","message":"Too many requests."}}`},
		{status: http.StatusOK, body: `{"api_key":"pp_live_after_backoff","user":{"id":"u-1","email":"creator@example.com"}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	token, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}
	if token.APIKey != "pp_live_after_backoff" {
		t.Errorf("token = %+v", token)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.seen != 2 {
		t.Errorf("polls = %d, want 2", srv.seen)
	}
}

// TestDeviceLoginSurvivesANetworkBlip: every deploy recreates the API container,
// so a poll landing in that window is a transport failure with nothing wrong
// behind it. Ending the login there would mean asking the user to start again for
// a reason that had nothing to do with them.
func TestDeviceLoginSurvivesANetworkBlip(t *testing.T) {
	const startBody = `{"device_code":"dc-secret","user_code":"BCDF-GHJK","verification_uri":"https://prepublish.ai/cli/login","verification_uri_complete":"https://prepublish.ai/cli/login?code=BCDF-GHJK","expires_in":600,"interval":1}`

	var (
		mu      sync.Mutex
		polls   int
		dropped bool
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/auth/start" {
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, startBody)
			return
		}

		mu.Lock()
		polls++
		first := !dropped
		dropped = true
		mu.Unlock()

		if first {
			// The connection dies without an answer: exactly what a container
			// restart looks like from here.
			panic(http.ErrAbortHandler)
		}
		io.WriteString(w, `{"api_key":"pp_live_after_blip","user":{"id":"u-1","email":"creator@example.com"}}`)
	}))
	defer ts.Close()

	token, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if err != nil {
		t.Fatalf("DeviceLogin gave up on a dropped connection: %v", err)
	}
	if token.APIKey != "pp_live_after_blip" {
		t.Errorf("token = %+v", token)
	}

	mu.Lock()
	defer mu.Unlock()
	if polls != 2 {
		t.Errorf("polls = %d, want the flow to try again after the failure", polls)
	}
}

// TestDeviceLoginEndsOnAnAnswerThatCannotChange is the other half of the retry
// rule: a server error with nothing to wait for is reported rather than retried,
// so a broken deployment does not look like a hung login.
func TestDeviceLoginEndsOnAnAnswerThatCannotChange(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusInternalServerError, body: `{"error":{"code":"INTERNAL_ERROR","message":"An internal error occurred."}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if api.StatusOf(err) != http.StatusInternalServerError {
		t.Fatalf("err = %v, want the 500 surfaced", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.seen != 1 {
		t.Errorf("polls = %d, want the flow to stop on a 500", srv.seen)
	}
}

// TestDeviceLoginEndsOnAConsumedRequest: INVALID_GRANT means the device code is
// unknown or already spent, and no amount of waiting changes that.
func TestDeviceLoginEndsOnAConsumedRequest(t *testing.T) {
	srv := &loginServer{polls: []pollAnswer{
		{status: http.StatusBadRequest, body: `{"error":{"code":"INVALID_GRANT","message":"This login request is no longer valid."}}`},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	_, err := DeviceLogin(context.Background(), api.New(ts.URL, "", "test"), "test-host", nil)
	if !api.IsCode(err, api.CodeInvalidGrant) {
		t.Fatalf("err = %v, want INVALID_GRANT", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.seen != 1 {
		t.Errorf("polls = %d, want the flow to stop", srv.seen)
	}
}
