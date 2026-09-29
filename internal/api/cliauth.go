package api

import "context"

// CLIAuthStart opens a device-authorization request
// (POST /api/cli/auth/start) and returns the codes to show the user.
//
// The route is public and rate-limited per IP. clientName is what the user sees
// in the approval page and in the key's dashboard name (`CLI · <client_name>`);
// it is clamped to 64 characters by the server.
func (c *Client) CLIAuthStart(ctx context.Context, clientName string) (*DeviceStart, error) {
	body := struct {
		ClientName string `json:"client_name,omitempty"`
	}{ClientName: clientName}

	var out DeviceStart
	if err := c.postJSON(ctx, "/api/cli/auth/start", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CLIAuthToken polls an approved request (POST /api/cli/auth/token) and returns
// the API key exactly once.
//
// While the request is still waiting the server answers 400 with the code
// "AUTHORIZATION_PENDING"; IsCode(err, "AUTHORIZATION_PENDING") is the way to
// tell that apart from a real failure. A denial is 403 ACCESS_DENIED, an
// expired request 410 EXPIRED_TOKEN, an unknown or already consumed one 400
// INVALID_GRANT, and a full key list 409 LIMIT_EXCEEDED.
func (c *Client) CLIAuthToken(ctx context.Context, deviceCode string) (*DeviceToken, error) {
	body := struct {
		DeviceCode string `json:"device_code"`
	}{DeviceCode: deviceCode}

	var out DeviceToken
	if err := c.postJSON(ctx, "/api/cli/auth/token", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
