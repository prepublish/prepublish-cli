package auth

import (
	"strings"
	"testing"

	"github.com/prepublish/prepublish-cli/internal/api"
)

func TestSafeStartKeepsATrustedServerPage(t *testing.T) {
	start := &api.DeviceStart{
		DeviceCode:              "device-secret",
		UserCode:                "BCDF-GHJK",
		VerificationURI:         "https://prepublish.ai/cli/login",
		VerificationURIComplete: "https://prepublish.ai/cli/login?code=BCDF-GHJK",
		ExpiresIn:               600,
		Interval:                2,
	}

	safe := SafeStart("https://prepublish.ai", start)

	if safe.VerificationURIComplete != start.VerificationURIComplete {
		t.Errorf("VerificationURIComplete = %q, want the server's own page", safe.VerificationURIComplete)
	}
	if safe.VerificationURI != start.VerificationURI {
		t.Errorf("VerificationURI = %q", safe.VerificationURI)
	}
	if safe.UserCode != "BCDF-GHJK" {
		t.Errorf("UserCode = %q", safe.UserCode)
	}
	if safe.ExpiresIn != 600 || safe.Interval != 2 {
		t.Errorf("ExpiresIn/Interval = %d/%d, want the server's values", safe.ExpiresIn, safe.Interval)
	}
	// The device code is the secret half of the pair and no screen needs it.
	if safe.DeviceCode != "" {
		t.Errorf("DeviceCode = %q, want it dropped from the display copy", safe.DeviceCode)
	}
	if start.DeviceCode == "" {
		t.Error("SafeStart cleared the caller's own structure instead of a copy")
	}
}

func TestSafeStartDropsAPageOnAnotherOrigin(t *testing.T) {
	// A compromised, misconfigured or plain hostile API: the code screen is the
	// one page a user is asked to type a credential into, so a lookalike there is
	// the whole attack. The URL is dropped and the page rebuilt from the app the
	// user configured.
	start := &api.DeviceStart{
		UserCode:                "BCDF-GHJK",
		VerificationURI:         "https://prepublish.ai.evil.example/cli/login",
		VerificationURIComplete: "https://prepublish.ai.evil.example/cli/login?code=BCDF-GHJK",
	}

	safe := SafeStart("https://prepublish.ai", start)

	if want := "https://prepublish.ai/cli/login?code=BCDF-GHJK"; safe.VerificationURIComplete != want {
		t.Errorf("VerificationURIComplete = %q, want %q", safe.VerificationURIComplete, want)
	}
	if safe.VerificationURI != "https://prepublish.ai/cli/login" {
		t.Errorf("VerificationURI = %q, want the page without the code", safe.VerificationURI)
	}
}

func TestSafeStartRejectsEveryUnusableURL(t *testing.T) {
	cases := map[string]string{
		"file scheme":        "file:///Applications/Calculator.app",
		"windows share":      `\\host\share\x.exe`,
		"plain http":         "http://prepublish.ai/cli/login",
		"option":             "-a/Applications/Calculator.app",
		"another app":        "https://accounts.example.com/authorize",
		"empty":              "",
		"no host":            "https:///cli/login",
		"control character":  "https://prepublish.ai/cli/login\nX",
		"javascript scheme":  "javascript:alert(1)",
		"unsupported scheme": "ftp://prepublish.ai/",
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			start := &api.DeviceStart{UserCode: "BCDF-GHJK", VerificationURIComplete: candidate}
			safe := SafeStart("https://prepublish.ai", start)
			if safe.VerificationURIComplete != "https://prepublish.ai/cli/login?code=BCDF-GHJK" {
				t.Errorf("VerificationURIComplete = %q, want the rebuilt page", safe.VerificationURIComplete)
			}
		})
	}
}

func TestSafeStartBuildsNothingFromAMalformedCode(t *testing.T) {
	// Neither the server's page nor the code can be trusted, so there is nothing
	// safe to show or open. An empty field is the honest answer: the caller says
	// so instead of sending the user somewhere unverified.
	start := &api.DeviceStart{
		UserCode:                "BCDF-GHJ\x1b[2J", // a control sequence, not a code
		VerificationURI:         "https://evil.example/cli/login",
		VerificationURIComplete: "https://evil.example/cli/login?code=x",
	}

	safe := SafeStart("https://prepublish.ai", start)

	if safe.VerificationURIComplete != "" || safe.VerificationURI != "" {
		t.Errorf("URLs = %q / %q, want both empty", safe.VerificationURIComplete, safe.VerificationURI)
	}
	if strings.ContainsAny(safe.UserCode, "\x1b\n\r") {
		t.Errorf("UserCode = %q, want the control characters stripped", safe.UserCode)
	}
	if strings.Contains(safe.UserCode, "-") == false {
		t.Errorf("UserCode = %q, want the readable part kept", safe.UserCode)
	}
}

func TestSafeStartRejectsALowercaseOrLookalikeCode(t *testing.T) {
	// The alphabet excludes the characters that are hard to tell apart when read
	// off a screen, so a code outside it did not come from this server's
	// generator.
	for _, code := range []string{"bcdf-ghjk", "BCDF-GHJ1", "ABCD-EFGH", "BCDFGHJK", "BCDF-GHJKX"} {
		start := &api.DeviceStart{UserCode: code, VerificationURI: "https://evil.example/x"}
		safe := SafeStart("https://prepublish.ai", start)
		if safe.VerificationURIComplete != "" {
			t.Errorf("code %q produced %q, want nothing built from an unexpected code", code, safe.VerificationURIComplete)
		}
	}
}

func TestSafeStartNil(t *testing.T) {
	if got := SafeStart("https://prepublish.ai", nil); got != nil {
		t.Errorf("SafeStart(nil) = %+v, want nil", got)
	}
}

func TestPrintableStripsTerminalControl(t *testing.T) {
	cases := map[string]string{
		"plain":                  "BCDF-GHJK",
		"ansi erase":             "BCDF\x1b[2J-GHJK",
		"carriage return":        "BCDF\r-GHJK",
		"newline is dropped":     "BCDF\nGHJK",
		"tab is dropped":         "BCDF\tGHJK",
		"right-to-left override": "BCDF\u202e-GHJK",
		"word joiner":            "BCDF\u2060-GHJK",
		"zero width space":       "BCDF\u200b-GHJK",
		"bell":                   "\aBCDF-GHJK",
		"backspace":              "BCDF\b-GHJK",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got := Printable(raw)
			if strings.ContainsAny(got, "\x1b\r\n\t\a\b\u202e\u2060\u200b") {
				t.Errorf("Printable(%q) = %q, want the invisible characters gone", raw, got)
			}
			if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
				t.Errorf("Printable(%q) = %q, want it trimmed", raw, got)
			}
		})
	}

	// Readable text survives, including non-Latin scripts: this is a terminal
	// safety filter, not a content filter.
	if got := Printable("  Код  BCDF  "); got != "Код BCDF" {
		t.Errorf("Printable = %q, want the readable text with the runs of space collapsed", got)
	}
	if got := Printable(strings.Repeat("x", 5000)); len([]rune(got)) != maxPrintable {
		t.Errorf("Printable kept %d runes, want them capped at %d", len([]rune(got)), maxPrintable)
	}
}

func TestOpenBrowserRefusesWhatTheLauncherWouldMisread(t *testing.T) {
	// These are checked before any process is started, so the test cannot launch
	// anything to find out.
	refused := []string{
		"",
		"   ",
		"-a/Applications/Calculator.app",
		"--version",
		`\\host\share\payload.exe`,
		"file:///etc/passwd",
		"javascript:alert(1)",
		"http://prepublish.ai/cli/login",
		"ftp://example.com/x",
		"https://prepublish.ai/\nnext",
	}
	for _, raw := range refused {
		t.Run(raw, func(t *testing.T) {
			if err := OpenBrowser(raw); err == nil {
				t.Errorf("OpenBrowser(%q) = nil, want a refusal", raw)
			}
		})
	}
}
