package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// errorWidth is the measure an error is wrapped at when the caller does not say
// one. Errors are short prose and the call site is often a bare
// `ui.RenderError(err)` inside an exit path, so the default is a comfortable
// prose column rather than the report's full 100: it fits an 80-column terminal
// without the terminal doing the wrapping.
const errorWidth = 78

// RenderError turns any error into something a person can act on: a title, the
// server's own sentence when there is one, and the command that fixes it. It
// never returns an empty string for a non-nil error, and it never panics on a
// nil or half-populated *api.APIError.
//
// Prefer RenderErrorWidth where the terminal width is known.
func RenderError(err error) string { return RenderErrorWidth(err, errorWidth) }

// RenderErrorWidth is RenderError at a caller-chosen width.
func RenderErrorWidth(err error, width int) string {
	if err == nil {
		return ""
	}
	w := contentWidth(width)

	view := classify(err)
	var b strings.Builder
	head := st.bad.Render("✖") + "  " + st.bold.Render(view.title)
	if view.meta != "" {
		meta := st.faint.Render(view.meta)
		if lipgloss.Width(head)+2+lipgloss.Width(meta) <= w {
			head = padRight(head, w-lipgloss.Width(meta)) + meta
		}
	}
	b.WriteString(head)
	for _, line := range wrap(strings.TrimSpace(view.detail), w-3) {
		b.WriteString("\n   " + st.dim.Render(line))
	}
	if view.action != "" {
		b.WriteString("\n   " + st.key.Render(view.action))
	}
	if view.note != "" {
		for _, line := range wrap(view.note, w-3) {
			b.WriteString("\n   " + st.faint.Render(line))
		}
	}
	return fit(b.String(), w)
}

// errorView is one rendered failure.
type errorView struct {
	title  string
	detail string
	action string // the command that fixes it, when there is one
	note   string // extra context, kept quiet
	meta   string // the code and status, in the corner
}

// classify is the whole mapping, in one place: every branch keys off a code or
// a status rather than a message, because a message is prose the server is free
// to rewrite.
func classify(err error) errorView {
	// The APIError check comes first, and it checks for a nil pointer: an error
	// interface can hold a typed nil, and both errors.Is and the pointer's own
	// methods would then panic on it. A nil APIError carries no code, no status
	// and no message, so it belongs in the generic branch anyway.
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		if apiErr == nil {
			return errorView{
				title:  "Something went wrong",
				detail: "The API returned no error details.",
				action: "prepublish --help",
			}
		}
		return classifyAPIError(apiErr)
	}

	// Cancellation is not a failure: it is the user pressing ctrl+c, and it
	// says so without an error glyph's alarm.
	if errors.Is(err, context.Canceled) {
		return errorView{title: "Canceled", detail: "Nothing was changed."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errorView{
			title:  "Timed out",
			detail: "The API did not answer in time. The audit may still be running on the server.",
			action: "prepublish history",
		}
	}

	// A transport error that never became an APIError: DNS, refused, TLS.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return networkView(urlErr.Err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return networkView(err)
	}

	return errorView{
		title:  "Something went wrong",
		detail: strings.TrimSpace(err.Error()),
		action: "prepublish --help",
	}
}

func classifyAPIError(e *api.APIError) errorView {
	meta := statusMeta(e)
	switch {
	case e.Status == 401 || api.IsCode(e, api.CodeUnauthorized):
		return errorView{
			title:  "Sign in required",
			detail: firstMessage(e.Message, "The credential this machine holds is missing, expired or revoked."),
			action: "prepublish login",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeFreeAnalysisUsed):
		return errorView{
			title:  "Today's audits are used up",
			detail: firstMessage(e.Message, "This tier's daily allowance is spent. It resets at midnight UTC."),
			action: "prepublish upgrade",
			note:   "Creator allows 50 audits a day, full reports, uploads and thumbnails.",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeRateLimited):
		detail := "The API is asking this client to slow down."
		if e.RetryAfter > 0 {
			detail = "The API is asking this client to slow down. Try again in " + wait(e.RetryAfter) + "."
		}
		return errorView{title: "Rate limited", detail: detail, meta: meta}
	case api.IsCode(e, api.CodeNoSubscription), api.IsCode(e, api.CodeSubscriptionRequired):
		return errorView{
			title:  "A plan is required for that",
			detail: firstMessage(e.Message, "Video and audio uploads, thumbnails and full reports need a paid plan."),
			action: "prepublish upgrade",
			note:   "Free checks still work: pass script text instead of a file.",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeEmailRequired):
		return errorView{
			title:  "An email address is needed",
			detail: firstMessage(e.Message, "This tool allows one anonymous check a day; an email address raises it to three."),
			action: "--email you@example.com",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeValidation) && strings.EqualFold(e.Field, "email"):
		return errorView{
			title:  "That email address was refused",
			detail: firstMessage(e.Message, "A valid email address is required to run a free audit."),
			action: "prepublish audit -f script.txt --email you@example.com",
			note:   "It is only used for the audit, and never shown to anyone else.",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeValidation):
		return errorView{
			title:  "The request was rejected",
			detail: firstMessage(e.Message, "The API rejected one of the values in the request.") + fieldNote(e.Field),
			meta:   meta,
		}
	case api.IsCode(e, api.CodeNotFound):
		return errorView{
			title:  "Not found",
			detail: firstMessage(e.Message, "The API has nothing under that id."),
			action: "prepublish history",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeLimitExceeded):
		return errorView{
			title:  "Too many API keys",
			detail: "This account already holds ten live API keys, which is the limit.",
			action: "Revoke one at " + appURL + "/dashboard",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeExpiredToken):
		return errorView{
			title:  "The sign-in request expired",
			detail: "The code is only valid for ten minutes, and it was not approved in time.",
			action: "prepublish login",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeInvalidGrant):
		return errorView{
			title:  "That sign-in request is no longer valid",
			detail: "The request was already used or was never started. Sign in again to get a fresh code.",
			action: "prepublish login",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeAccessDenied):
		return errorView{
			title:  "The browser request was denied",
			detail: "Someone declined the sign-in in the browser, so no key was issued.",
			action: "prepublish login",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeAuthorizationPending):
		return errorView{
			title:  "Waiting for approval",
			detail: "The sign-in request has not been approved in the browser yet.",
			action: "prepublish login",
			meta:   meta,
		}
	case api.IsCode(e, api.CodeNetworkError) || e.Status == 0:
		view := networkView(errors.Unwrap(e))
		view.meta = meta
		return view
	case api.IsCode(e, api.CodeUnexpectedResponse) || e.Status >= 500:
		return errorView{
			title:  "The API answered with something unexpected",
			detail: firstMessage(e.Message, "This version of the CLI could not read the response. It is usually a server-side problem, and retrying helps."),
			action: "prepublish version",
			meta:   meta,
		}
	case e.Status == 403:
		return errorView{
			title:  "Not allowed",
			detail: firstMessage(e.Message, "This credential is not allowed to do that."),
			action: "prepublish login",
			meta:   meta,
		}
	default:
		return errorView{
			title:  "The request failed",
			detail: firstMessage(e.Message, "The API refused the request."),
			meta:   meta,
		}
	}
}

// networkView is the "we never reached the API" case. It is the most common
// failure in the wild (a laptop on a captive portal, a VPN that drops), and the
// least useful one to report as a stack trace.
func networkView(cause error) errorView {
	detail := "The API could not be reached. Check the connection, then try again."
	if u := urlOf(cause); u != "" {
		detail = "The API at " + u + " could not be reached. Check the connection, then try again."
	}
	return errorView{
		title:  "Can't reach the API",
		detail: detail,
		action: "--api-url https://api.prepublish.ai",
		note:   "A proxy, a VPN or a captive portal is the usual cause.",
	}
}

// urlOf digs the host out of a transport error, so the reader knows which
// endpoint was unreachable (a --api-url override is otherwise invisible).
func urlOf(err error) string {
	if err == nil {
		return ""
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return strings.TrimSpace(urlErr.URL)
	}
	return ""
}

func statusMeta(e *api.APIError) string {
	var parts []string
	if e.Status > 0 {
		parts = append(parts, strconv.Itoa(e.Status))
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	return strings.Join(parts, " · ")
}

func fieldNote(field string) string {
	if strings.TrimSpace(field) == "" {
		return ""
	}
	return " The field it named is `" + field + "`."
}

func firstMessage(server, fallback string) string {
	if s := strings.TrimSpace(server); s != "" {
		return s
	}
	return fallback
}

func wait(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds < 90 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm", (seconds+59)/60)
}
