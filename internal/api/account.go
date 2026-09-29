package api

import "context"

// Me returns the account behind the client's credential
// (GET /api/auth/me). It is the cheapest way to prove a stored key still
// works: an invalid or revoked key that would be ignored under OptionalAuth
// is a hard 401 here.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var out struct {
		User *User `json:"user"`
	}
	if err := c.getJSON(ctx, "/api/auth/me", &out); err != nil {
		return nil, err
	}
	if out.User == nil {
		return nil, &APIError{
			Status:  200,
			Code:    CodeUnexpectedResponse,
			Message: "the API returned no user for this credential",
		}
	}
	return out.User, nil
}

// Usage returns today's audit quota for the signed-in account
// (GET /api/user/usage). This is also where the plan tier comes from:
// /api/auth/me carries the Polar subscription status but not the tier.
func (c *Client) Usage(ctx context.Context) (*Usage, error) {
	var out Usage
	if err := c.getJSON(ctx, "/api/user/usage", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CheckFree returns the anonymous quota (GET /api/check-free).
//
// The route has no auth middleware, so it always answers for the caller's IP;
// anonID narrows it to this installation's own anonymous usage. A signed-in
// caller should use Usage instead.
func (c *Client) CheckFree(ctx context.Context, anonID string) (*CheckFree, error) {
	var out CheckFree
	path := withQuery("/api/check-free", map[string]string{"anonymous_user_id": anonID})
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClaimAnalyses attaches the audits run under an anonymous id to the signed-in
// account (POST /api/user/claim-analyses) and returns how many moved. It is
// what makes a `prepublish audit` run before `prepublish login` show up in the
// account afterwards, so login calls it.
func (c *Client) ClaimAnalyses(ctx context.Context, anonID string) (int, error) {
	body := struct {
		AnonymousUserID string `json:"anonymous_user_id"`
	}{AnonymousUserID: anonID}

	var out struct {
		Claimed int    `json:"claimed"`
		Message string `json:"message"`
	}
	if err := c.postJSON(ctx, "/api/user/claim-analyses", body, &out); err != nil {
		return 0, err
	}
	return out.Claimed, nil
}

// Subscription returns the account's Polar subscription
// (GET /api/user/subscription).
func (c *Client) Subscription(ctx context.Context) (*Subscription, error) {
	var out Subscription
	if err := c.getJSON(ctx, "/api/user/subscription", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BillingPortalURL returns the Polar customer portal URL for the account
// (GET /api/user/billing), which is where a plan is changed or cancelled.
func (c *Client) BillingPortalURL(ctx context.Context) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.getJSON(ctx, "/api/user/billing", &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// RevokeCurrentKey revokes the key the client is presenting
// (DELETE /api/user/api-keys/current).
//
// The server only accepts this from an API key, not from a browser session, and
// it is what makes `prepublish logout` clean up after itself instead of leaving
// a live key behind in the dashboard. A key that has already been revoked
// answers 401, which logout treats as success.
func (c *Client) RevokeCurrentKey(ctx context.Context) error {
	return c.delete(ctx, "/api/user/api-keys/current")
}
