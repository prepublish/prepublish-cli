package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// defaultInterval bounds how often the CLI polls when the server does not say.
// Two seconds is the server's own advertised interval: fast enough that
// approval feels immediate, slow enough not to look like a hot loop.
const defaultInterval = 2 * time.Second

// defaultTTL is the fallback lifetime of a login request when the server omits
// expires_in. It matches the backend's 10-minute TTL, and exists only so a
// malformed start response cannot leave the CLI polling forever.
const defaultTTL = 10 * time.Minute

// maxInterval caps how far the poll interval backs off. Long enough that a rate
// limit or an outage is respected without the login stalling well past the
// moment the user approves, short enough to stay inside the request's lifetime.
const maxInterval = 30 * time.Second

// LoginEventKind tells a UI which step the flow reached.
type LoginEventKind int

// The steps a login reports.
const (
	// Started is emitted once, as soon as the codes exist and before the first
	// poll. It carries the DeviceStart the UI shows: the user code and the URL.
	Started LoginEventKind = iota
	// Polling is emitted after each poll that found the request still waiting,
	// so a spinner or an elapsed-time readout has something to advance on.
	Polling
)

// LoginEvent is one step of the flow.
type LoginEvent struct {
	Kind LoginEventKind
	// Start carries the codes. It is set on Started and re-sent on Polling so
	// a UI that missed the first event (a reader that subscribed late, a screen
	// that was re-created) still has what it needs to render the code.
	Start *api.DeviceStart
}

// DeviceLogin runs the full device-authorization flow: it opens nothing by
// itself (that is the caller's decision, and the caller owns the terminal), but
// it starts a request, reports the codes, and polls until the user approves it
// in the browser, denies it, or the request expires.
//
// It returns the issued API key exactly once. Errors are ErrDenied and
// ErrExpired for the two expected outcomes, ctx.Err() when the caller gives up
// (Ctrl-C), and the *api.APIError for anything else — a 409 LIMIT_EXCEEDED when
// the account already holds ten live keys, or a request the server has already
// consumed.
//
// Polling is patient on purpose. Approval takes as long as it takes: the user
// has to reach their mail, sign in and read a code off the screen, and everything
// between here and there is a network that can fail. A rate limit is waited out
// and a failed poll is retried, both backing off, until the request's own
// deadline. Only an answer that will never change — denied, expired, unknown,
// over the key limit — ends the flow early.
//
// events may be nil, which is the non-interactive case: the caller has already
// printed the codes and only wants the key. When it is not nil, the flow never
// drops an event and never blocks on one: a send waits for the reader or for
// ctx, so a UI that stops reading cannot leak the polling goroutine.
func DeviceLogin(ctx context.Context, client *api.Client, clientName string, events chan<- LoginEvent) (*api.DeviceToken, error) {
	start, err := client.CLIAuthStart(ctx, clientName)
	if err != nil {
		return nil, err
	}
	if start.DeviceCode == "" || start.UserCode == "" {
		return nil, errors.New("the API returned an incomplete login request")
	}
	if !emit(ctx, events, LoginEvent{Kind: Started, Start: start}) {
		return nil, ctx.Err()
	}

	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	ttl := time.Duration(start.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultTTL
	}
	deadline := time.Now().Add(ttl)

	// wait is how long to hold off before the next poll. It starts at the
	// server's interval and only ever grows, so a server that is struggling is
	// asked less often instead of more.
	wait := interval
	for {
		// Wait before the first poll as well: the user has to reach the
		// browser, so an immediate poll can only ever be answered
		// AUTHORIZATION_PENDING and would spend one of the endpoint's requests
		// for nothing.
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}

		token, err := client.CLIAuthToken(ctx, start.DeviceCode)
		switch {
		case err == nil:
			return token, nil
		case api.IsCode(err, api.CodeAuthorizationPending):
			// Still waiting: the expected case, not an error.
			wait = interval
		case api.IsCode(err, api.CodeAccessDenied):
			return nil, ErrDenied
		case api.IsCode(err, api.CodeExpiredToken):
			return nil, ErrExpired
		case retryablePoll(err):
			// The request is still open; the answer just did not arrive. Slow
			// down and try again inside the request's lifetime.
			interval = minDuration(2*interval, maxInterval)
			wait = interval
			if retryAfter := api.RetryAfterOf(err); retryAfter > 0 {
				wait = retryAfter
			}
			if remaining := time.Until(deadline); wait > remaining {
				wait = remaining
			}
			if wait <= 0 {
				return nil, ErrExpired
			}
		default:
			return nil, err
		}

		if !emit(ctx, events, LoginEvent{Kind: Polling, Start: start}) {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			// The client's own deadline. The server would answer
			// EXPIRED_TOKEN on the next poll, but at the boundary the user has
			// already waited long enough to be told.
			return nil, ErrExpired
		}
	}
}

// retryablePoll reports whether a failed poll is worth repeating before the
// request expires.
//
// Two things qualify. A rate limit is the server asking to be asked less often,
// which is exactly what the caller does next — the per-IP budget on this route is
// small, and a user who takes two minutes to approve used to be cut off mid-login
// by a 429 that DeviceLogin treated as final. A transport failure or a gateway
// answer is the API being redeployed or briefly overloaded, which has nothing to
// do with the request, and the request stays open for ten minutes.
func retryablePoll(err error) bool {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Status {
	case 0, // never reached the API
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// emit delivers one event. It returns false only when the context ended while
// waiting for a reader, which the caller reports as cancellation.
func emit(ctx context.Context, events chan<- LoginEvent, ev LoginEvent) bool {
	if events == nil {
		return true
	}
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// sleep waits for d or for the context to end.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
