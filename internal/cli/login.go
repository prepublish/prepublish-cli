package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/auth"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/tui"
	"github.com/prepublish/prepublish-cli/internal/ui"
	"github.com/prepublish/prepublish-cli/internal/version"
)

// tokenStdinLimit bounds how much of stdin `login --with-token` will read. A
// key is 40 characters; anything past this is not a key.
const tokenStdinLimit = 8 << 10

type loginOptions struct {
	withToken bool
	noBrowser bool
}

func (a *app) loginCmd() *cobra.Command {
	opts := &loginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in, in a browser or with an API key",
		Long: "Sign in to use the paid features, keep a history and raise the daily\n" +
			"allowance from three audits to fifty.\n\n" +
			"The default flow is the browser one: the CLI shows a short code, opens\n" +
			"prepublish.ai/cli/login, and waits for you to approve this machine. The\n" +
			"approval mints an API key named after this machine and the CLI stores it in\n" +
			"credentials.json, mode 0600. Audits this machine ran anonymously are moved\n" +
			"onto the account.\n\n" +
			"--with-token takes a key you created yourself in the dashboard instead, so\n" +
			"the CLI never needs a browser (CI, a remote shell).",
		Example: "  prepublish login\n" +
			"  prepublish login --no-browser\n" +
			"  printf '%s' \"$PREPUBLISH_KEY\" | prepublish login --with-token",
		Args: a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return a.login(ctxOf(cmd), cmd, *opts) },
	}
	cmd.Flags().BoolVar(&opts.withToken, "with-token", false,
		"read an API key from stdin instead of running the browser flow")
	cmd.Flags().BoolVar(&opts.noBrowser, "no-browser", false,
		"print the code and the URL instead of opening a browser")
	return cmd
}

// login signs in, then leaves the CLI holding a working credential either way.
func (a *app) login(ctx context.Context, cmd *cobra.Command, o loginOptions) error {
	if o.withToken {
		return a.loginWithToken(ctx, cmd)
	}

	if a.signedIn() {
		who := a.whoLine()
		if !a.interactive() {
			a.say(ui.Info("Already signed in as " + who + "; nothing to do"))
			return nil
		}
		again, err := tui.Confirm("You are already signed in as " + who + ". Sign in again?")
		if err != nil {
			return err
		}
		if !again {
			a.say(ui.Info("Kept the existing credentials"))
			return nil
		}
	}

	// The login request itself is anonymous: sending a stale key would only
	// give the server something to reject on a route that does not need one.
	clientName := "prepublish-cli on " + a.host

	var (
		token *api.DeviceToken
		err   error
	)
	if a.interactive() && !o.noBrowser {
		token, err = tui.RunLogin(ctx, a.deviceLogin(ctx, clientName))
	} else {
		token, err = a.plainLogin(ctx, a.deviceLogin(ctx, clientName), o.noBrowser)
	}
	if err != nil {
		return loginErr(err)
	}

	creds := &config.Credentials{
		APIKey:    token.APIKey,
		KeyID:     token.KeyID,
		KeyPrefix: token.KeyPrefix,
		APIURL:    a.apiURL,
		Email:     token.User.Email,
		Source:    config.LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}
	return a.finishLogin(ctx, creds, nil)
}

// deviceLogin runs the device flow and cleans every event before any UI sees it.
//
// The flow's events carry strings straight off the network, and a UI does two
// dangerous things with them: it hands a URL to the system's browser launcher,
// and it prints a code to a terminal. Relaying the events through SafeStart is
// what keeps both decisions here, in one place, instead of in each renderer —
// the TUI and the plain-text path read identically sanitized events and cannot
// disagree about which URL is trustworthy.
//
// The relay goroutine exists because the flow writes to the channel and the UI
// reads from it: something has to sit between them. It ends when the flow closes
// the channel or the context is cancelled, both of which DeviceLogin guarantees
// before it returns, so the wait below cannot hang.
func (a *app) deviceLogin(ctx context.Context, clientName string) func(context.Context, chan<- auth.LoginEvent) (*api.DeviceToken, error) {
	return func(ctx context.Context, events chan<- auth.LoginEvent) (*api.DeviceToken, error) {
		appURL := a.cfg.AppURL()
		raw := make(chan auth.LoginEvent, 4)
		relayed := make(chan struct{})

		go func() {
			defer close(relayed)
			for event := range raw {
				if event.Start != nil {
					event.Start = auth.SafeStart(appURL, event.Start)
				}
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}()

		token, err := auth.DeviceLogin(ctx, a.anonymousClient(), clientName, raw)
		close(raw)
		<-relayed
		return token, err
	}
}

// plainLogin runs the device flow without a full-screen program: the URL and the
// code are printed once, the browser is opened unless asked not to, and the wait
// is announced once. Polling is not narrated — a line per poll says nothing the
// user cannot already see, and it buries the code they were told to read.
func (a *app) plainLogin(
	ctx context.Context,
	login func(context.Context, chan<- auth.LoginEvent) (*api.DeviceToken, error),
	noBrowser bool,
) (*api.DeviceToken, error) {
	events := make(chan auth.LoginEvent, 1)
	type outcome struct {
		token *api.DeviceToken
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		token, err := login(ctx, events)
		done <- outcome{token: token, err: err}
	}()

	announced := false
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-done:
			return result.token, result.err
		case event := <-events:
			if announced || event.Start == nil {
				continue
			}
			announced = true
			a.printDeviceCode(event.Start)
			if !noBrowser {
				if err := auth.OpenBrowser(event.Start.VerificationURIComplete); err != nil {
					a.warn(ui.Warn(err.Error()))
				}
			}
		}
	}
}

// printDeviceCode is the whole instruction set for a terminal without a TUI:
// the URL to approve on, the code to check it against, and one line saying the
// CLI is waiting. It goes through say, so machine mode keeps stdout clean.
//
// The values are already sanitized by deviceLogin; the empty case is the one the
// sanitizer can produce, when a response's own URL did not point at the
// configured app and its code was not in the expected shape. Saying that is more
// useful than printing a blank line.
func (a *app) printDeviceCode(start *api.DeviceStart) {
	url := firstNonEmpty(start.VerificationURIComplete, start.VerificationURI)
	if url == "" {
		url = "(the API did not report a usable approval page)"
	}

	a.say("")
	a.say("Open this URL to approve this machine:")
	a.say("")
	a.say("  " + url)
	a.say("")
	a.say("Code: " + start.UserCode + " (check it matches the page)")
	a.say("")
	a.say("waiting for approval…")
}

// loginWithToken stores a key the user pasted or piped in. It is the only way
// to sign in on a machine without a browser, and the way CI authenticates.
func (a *app) loginWithToken(ctx context.Context, cmd *cobra.Command) error {
	if stdinIsTTY() {
		return usageErr(errors.New(
			"no key on stdin: use the browser flow, or pipe one in with `pbpaste | prepublish login --with-token`"))
	}

	data, err := io.ReadAll(io.LimitReader(os.Stdin, tokenStdinLimit))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return usageErr(errors.New("no API key on stdin"))
	}

	// Validate before storing: a key that does not work should leave the
	// previous sign-in untouched. Me is the one authenticated route that cannot
	// answer "fine" for a revoked key.
	probe := api.New(a.apiURL, key, version.UserAgent())
	user, err := probe.Me(ctx)
	if err != nil {
		return err
	}

	creds := &config.Credentials{
		APIKey:    key,
		APIURL:    a.apiURL,
		Email:     user.Email,
		Source:    config.LoginSourceToken,
		CreatedAt: time.Now().UTC(),
	}
	return a.finishLogin(ctx, creds, user)
}

// finishLogin is the second half of a sign-in, shared by both flows: store the
// key, prove it, move earlier anonymous audits over, and show the account.
//
// user may carry an identity that was already verified (the --with-token path,
// which validated the key before saving it); otherwise the stored key is
// checked with Me, which is also what catches a key that expired between the
// browser approval and this call.
func (a *app) finishLogin(ctx context.Context, creds *config.Credentials, user *api.User) error {
	if strings.TrimSpace(creds.APIKey) == "" {
		return errors.New("the API returned an empty key")
	}
	if creds.CreatedAt.IsZero() {
		creds.CreatedAt = time.Now().UTC()
	}
	if creds.APIURL == "" {
		creds.APIURL = a.apiURL
	}

	a.revokeReplaced(ctx, creds.APIKey)

	if err := config.SaveCredentials(creds); err != nil {
		return err
	}
	a.adopt(creds)

	if user == nil {
		verified, err := a.client.Me(ctx)
		if err != nil {
			return err
		}
		user = verified
	}

	// Remember the address: free tool calls and later audits can use it without
	// another round trip, and free audits require one.
	remembered := false
	if creds.Email == "" && user.Email != "" {
		creds.Email = user.Email
		remembered = true
	}
	if a.cfg.Email() == "" && user.Email != "" {
		a.cfg.SetEmail(user.Email)
		if err := a.cfg.Save(); err != nil {
			a.warn(ui.Warn("could not remember the email: " + err.Error()))
		}
	}
	if remembered {
		if err := config.SaveCredentials(creds); err != nil {
			a.warn(ui.Warn("could not update the credentials file: " + err.Error()))
		}
	}

	a.claimAnonymous(ctx)

	// The card is worth a round trip, but a failure here must not undo a
	// sign-in that already worked.
	usage, err := a.client.Usage(ctx)
	if err != nil {
		usage = nil
	}

	if a.jsonMode {
		return a.printJSON(struct {
			User   *api.User  `json:"user"`
			Usage  *api.Usage `json:"usage,omitempty"`
			Source string     `json:"source"`
		}{User: user, Usage: usage, Source: string(creds.Source)})
	}

	a.say(ui.Success("Signed in as " + user.Email))
	a.result(ui.RenderAccount(user, usage, nil, config.SourceFile, a.width))
	return nil
}

// revokeReplaced retires the key this sign-in replaces.
//
// Signing in again is the normal way to refresh a key, and without this the
// previous one stays live on the server forever: it keeps a slot in the account's
// ten-key limit and it is a credential nobody is watching any more. Only a key
// this CLI stored is revoked — an environment key belongs to whoever exported it,
// and revoking it from here would break a CI job that never asked for it. A key
// bound to another API is never sent to this one.
func (a *app) revokeReplaced(ctx context.Context, nextKey string) {
	old := a.creds
	if a.source != config.SourceFile || old == nil || old.APIKey == "" || old.APIKey == nextKey {
		return
	}
	if !config.SameOrigin(old.BoundURL(), a.apiURL) {
		return
	}

	previous := api.New(a.apiURL, old.APIKey, version.UserAgent())
	if err := previous.RevokeCurrentKey(ctx); err != nil {
		switch api.StatusOf(err) {
		case http.StatusUnauthorized, http.StatusNotFound:
			// Already revoked, or never existed. The goal is reached.
		default:
			a.warn(ui.Warn("could not revoke the previous key: " + err.Error()))
			a.warn(ui.Info("it is still listed at " + a.cfg.AppURL() + "/dashboard"))
		}
	}
}

// claimAnonymous moves the audits this machine ran before signing in onto the
// account. A failure is a warning: the audits still exist, they are just still
// attached to the anonymous id.
func (a *app) claimAnonymous(ctx context.Context) {
	anonID := strings.TrimSpace(a.cfg.AnonymousID())
	if anonID == "" {
		return
	}
	claimed, err := a.client.ClaimAnalyses(ctx, anonID)
	switch {
	case err != nil:
		a.warn(ui.Warn("could not claim earlier anonymous audits: " + err.Error()))
	case claimed > 0:
		a.say(ui.Success("claimed " + count(claimed, "earlier audit") + " from this machine"))
	}
}

// whoLine names the current account for a prompt, without a network call.
func (a *app) whoLine() string {
	if email := a.credsEmail(); email != "" {
		return email
	}
	return "this account"
}

// loginErr turns the two expected outcomes of the device flow into exit codes
// that tell a script to try again.
func loginErr(err error) error {
	switch {
	case errors.Is(err, auth.ErrDenied):
		return exitErr(ExitAuth, errors.New("the request was denied in the browser"))
	case errors.Is(err, auth.ErrExpired):
		return exitErr(ExitAuth, err)
	default:
		return err
	}
}
