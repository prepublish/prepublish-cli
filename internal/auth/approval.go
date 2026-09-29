package auth

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
)

// userCodeShape is the format the server guarantees for a user code: eight
// characters from an alphabet with no look-alikes, shown as two groups of four.
//
// It is used as a gate, not as a parser. A code that does not match is not turned
// into a URL, because the only way to build an approval link without the server's
// own is to put the code in it.
var userCodeShape = regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)

// maxPrintable bounds a server string before it is shown. Nothing legitimate in
// a start response is longer than a URL, and the cap keeps a response from
// pushing a screenful of text into a prompt.
const maxPrintable = 512

// SafeStart returns the part of a start response that is safe to show a user and
// to hand to a browser.
//
// Everything in it came from the network, and two of the fields are used in ways
// that matter:
//
//   - the URLs go to the OS's URL handler, which on Windows opens local files
//     and UNC paths as readily as web pages, and everywhere else reads a leading
//     "-" as an option. They are therefore used only when they are https (or
//     http on loopback, for a local app) and point at the same origin as the
//     configured app URL. Anything else is dropped and rebuilt locally from the
//     app URL and the code.
//   - the code and the URLs are printed to a terminal, where a control or bidi
//     character can move the cursor, repaint the line, or reverse the text
//     around it. Printable strips those before any renderer sees them.
//
// The device code is dropped from the copy: it is the secret half of the pair and
// no view needs it. Polling uses the response's own values.
func SafeStart(appURL string, start *api.DeviceStart) *api.DeviceStart {
	if start == nil {
		return nil
	}

	safe := *start
	safe.DeviceCode = ""
	safe.UserCode = Printable(start.UserCode)
	safe.VerificationURI = trustedURL(appURL, start.VerificationURI)
	safe.VerificationURIComplete = trustedURL(appURL, start.VerificationURIComplete)

	// A response whose complete URI cannot be trusted still has a code the user
	// can type, so the approval page is rebuilt from the configured app and that
	// code. When even that is not possible the field stays empty and the caller
	// shows nothing rather than something unverified.
	if safe.VerificationURIComplete == "" {
		safe.VerificationURIComplete = approvalURL(appURL, safe.UserCode)
	}
	if safe.VerificationURI == "" {
		safe.VerificationURI = withoutQuery(safe.VerificationURIComplete)
	}
	return &safe
}

// Printable removes the characters that let a server string rewrite a terminal:
// C0 and C1 controls (cursor movement, OSC sequences), the invisible formatting
// characters that can reorder or hide text, and the line separators. Any other
// whitespace collapses to a single space, so a padded or multi-line value cannot
// push the rest of a screen around, and the result is capped and trimmed.
//
// It is not a content filter: anything a person can read survives, including
// non-Latin scripts.
func Printable(s string) string {
	out := make([]rune, 0, len(s))
	space := false
	for _, r := range s {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == '\u2028', r == '\u2029':
			continue
		case unicode.IsSpace(r):
			if len(out) == 0 || space {
				// Leading space is dropped, and a run of whitespace becomes one
				// space.
				continue
			}
			out = append(out, ' ')
			space = true
			continue
		default:
			out = append(out, r)
			space = false
		}
		if len(out) == maxPrintable {
			break
		}
	}
	return strings.TrimRight(string(out), " ")
}

// trustedURL returns candidate when it is a URL this CLI is willing to open and
// it belongs to the configured app; otherwise "".
//
// The app check is what stops a compromised or merely misconfigured API from
// sending the user to a page it chose: the code screen is the one place a user is
// expected to type a credential, so a lookalike page there is the whole attack.
//
// A candidate carrying an invisible character is refused rather than cleaned. A
// newline inside a URL splits it into a real address and a trailing fragment, and
// quietly joining the halves would mean the address the user reads is not the
// address that was sent.
func trustedURL(appURL, candidate string) string {
	value := strings.TrimSpace(candidate)
	if value == "" || strings.HasPrefix(value, "-") {
		return ""
	}
	if invisible(value) {
		return ""
	}
	if err := config.ValidateServiceURL(value); err != nil {
		return ""
	}
	if !config.SameOrigin(appURL, value) {
		return ""
	}
	return value
}

// invisible reports whether s contains a character that has no business in a
// URL: a control character (which can rewrite a terminal or split a line), an
// invisible formatting character (which can reverse or hide text), or
// whitespace (which cannot appear in a URL and would only be there to obscure
// it).
func invisible(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// approvalURL builds the approval page from the configured app URL and the user
// code. It returns "" when the code is not in the shape the server promises,
// since interpolating an unchecked value into a URL is not worth doing for a
// response that is already suspect.
func approvalURL(appURL, userCode string) string {
	code := strings.TrimSpace(userCode)
	if !userCodeShape.MatchString(code) {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" {
		return ""
	}
	return base + "/cli/login?code=" + url.QueryEscape(code)
}

// withoutQuery returns raw with its query and fragment removed, so a page link
// can be derived from a complete one. It returns "" when raw is not a URL or the
// result would be empty.
func withoutQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
