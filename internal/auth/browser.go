// Package auth runs the browser login flow: it opens the approval page, polls
// the API for the issued key and reports progress to whatever UI is watching.
package auth

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/prepublish/prepublish-cli/internal/config"
)

// ErrDenied is returned when the user cancels the request in the browser.
var ErrDenied = errors.New("login was denied in the browser")

// ErrExpired is returned when the request ran out of time before it was
// approved. The user has to start again; the codes are single-use.
var ErrExpired = errors.New("the login request expired; run `prepublish login` again")

// OpenBrowser opens url in the user's default browser.
//
// The URL is checked first, because this is the CLI's one path from a string to
// an OS handler. `open`, `xdg-open` and Windows' FileProtocolHandler all treat
// their argument as more than a web address: a leading "-" is an option (one of
// which runs a command on macOS), on Windows a file path, a UNC share, or an
// executable runs as readily as a page, and every launcher happily resolves a
// non-web scheme. Accepting only https — or http on a loopback host, for a local
// app — leaves nothing for those behaviours to act on.
//
// It is otherwise best-effort by design. The command is spawned and not waited
// on, so the caller never hangs behind a browser that is slow to start, and a
// headless machine (a server, an SSH session, a container) does not fail the
// login: the caller prints the URL too, and the user can open it elsewhere.
func OpenBrowser(url string) error {
	value := strings.TrimSpace(url)
	if value == "" {
		return errors.New("no URL to open")
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("refusing to open %q: it starts with a dash, which the system launcher would read as an option", value)
	}
	if err := config.ValidateServiceURL(value); err != nil {
		return fmt.Errorf("refusing to open %s", err)
	}

	var (
		launcher string
		args     []string
	)
	switch runtime.GOOS {
	case "darwin":
		launcher, args = "open", []string{value}
	case "windows":
		// rundll32 hands the URL to the shell directly, without going through
		// cmd.exe's argument parsing.
		launcher, args = "rundll32", []string{"url.dll,FileProtocolHandler", value}
	default:
		launcher, args = "xdg-open", []string{value}
	}

	if err := exec.Command(launcher, args...).Start(); err != nil {
		return fmt.Errorf("could not open a browser with %s: %w (open %s)", launcher, err, value)
	}
	return nil
}
