package api

import "context"

// HookAnalyze runs the hook analyzer (POST /api/tools/hook-analyzer).
//
// niche is optional. email promotes an anonymous caller from one analysis a day
// to three: without it, an exhausted anonymous quota answers 402 with the code
// "email_required" instead of the usual envelope, which is why IsCode is
// case-insensitive text rather than a number lookup.
func (c *Client) HookAnalyze(ctx context.Context, text, niche, email string) (*HookResult, error) {
	body := struct {
		HookText string `json:"hook_text"`
		Niche    string `json:"niche,omitempty"`
		Email    string `json:"email,omitempty"`
	}{HookText: text, Niche: niche, Email: email}

	var out HookResult
	if err := c.postJSON(ctx, "/api/tools/hook-analyzer", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AuthenticityCheck runs the standalone inauthentic-content check
// (POST /api/tools/authenticity-check). It is the free checker, independent of
// the audit flow; a full report carries its own authenticity section.
func (c *Client) AuthenticityCheck(ctx context.Context, title, script string) (*AuthenticityResult, error) {
	body := struct {
		VideoTitle string `json:"video_title"`
		ScriptText string `json:"script_text"`
	}{VideoTitle: title, ScriptText: script}

	var out AuthenticityResult
	if err := c.postJSON(ctx, "/api/tools/authenticity-check", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PolicyPreflight runs the monetization and policy pre-flight
// (POST /api/tools/policy-preflight).
//
// The body's field order and names are the server's: the script is "script",
// not "script_text", and the title is optional.
func (c *Client) PolicyPreflight(ctx context.Context, title, script string) (*PolicyResult, error) {
	body := struct {
		Script string `json:"script"`
		Title  string `json:"title,omitempty"`
	}{Script: script, Title: title}

	var out PolicyResult
	if err := c.postJSON(ctx, "/api/tools/policy-preflight", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
