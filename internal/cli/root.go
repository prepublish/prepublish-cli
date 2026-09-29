// Package cli is the command layer: cobra commands, flags, prompts, and the
// exit codes a shell sees.
//
// It owns everything between internal/api and the terminal. Commands never
// format a report themselves: they resolve what to send, hand the result to
// internal/ui for text and to internal/tui when a terminal is attached, and
// translate failures into one of the documented exit codes.
//
// Two rules run through the package:
//
//   - Machine mode wins. With --json (or a non-terminal stdout) nothing
//     prompts, nothing takes over the screen, and the API's own structs are
//     printed as JSON so a script can parse them.
//   - A missing credential is a product state, not a crash. Anonymous use is
//     supported everywhere except the paid paths, which say so and name the
//     command that fixes it.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"charm.land/fang/v2"
	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/tui"
	"github.com/prepublish/prepublish-cli/internal/ui"
	"github.com/prepublish/prepublish-cli/internal/version"
)

// Process exit codes. They are part of the CLI's contract with scripts, so they
// are grouped by what the caller should do next rather than by what went wrong:
// 2 fix the command line, 3 sign in, 4 upgrade or wait for the quota, 1
// anything else. 130 is not in the original contract; it is the shell's own
// convention for a process stopped by SIGINT, and reporting it lets a wrapper
// distinguish "the user pressed Ctrl-C" from a real failure.
const (
	ExitOK          = 0
	ExitFailure     = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitLimit       = 4
	ExitInterrupted = 130
)

// ExitError attaches a process exit code to an error.
//
// Commands return one when the failure has an obvious next action for the
// caller (pass a flag, sign in, upgrade). Everything else is left unwrapped and
// mapped from its API code by ExitCode.
type ExitError struct {
	Code int
	Err  error
	// Reported marks a failure the command has already explained in full — a
	// timeout that printed the audit's id and the next step. The error handler
	// then contributes nothing but the exit code, instead of printing the same
	// news twice in a different voice.
	Reported bool
}

// Error implements error.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error, so errors.Is/errors.As keep working through
// the exit code.
func (e *ExitError) Unwrap() error { return e.Err }

// exitErr builds an ExitError. It is the only constructor, so no code path can
// carry a nil error by accident.
func exitErr(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// usageErr marks a failure the caller can fix by changing what they passed:
// a missing title, an address nowhere on the machine, a container the API
// cannot transcribe.
//
// It exits 2 like a bad flag does, but it deliberately does not ask for the
// usage text to follow: the message already names the flag or the value that
// fixes it, and dumping the command's whole flag list under "an email is
// required" buries the one line the user needs.
func usageErr(err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: ExitUsage, Err: err}
}

// ExitCode maps what fang returned to the process status.
//
// An ExitError wins because it carries a decision the command already made. API
// errors are mapped by code before status: a 403 NO_SUBSCRIPTION is a plan
// limit, not a credential problem, even though 403 usually means "sign in".
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, huh.ErrUserAborted) {
		return ExitInterrupted
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return codeForAPIError(apiErr)
	}
	return ExitFailure
}

// codeForAPIError maps one API failure to the exit code that tells a script
// what to do about it.
func codeForAPIError(e *api.APIError) int {
	switch {
	case api.IsCode(e, api.CodeFreeAnalysisUsed),
		api.IsCode(e, api.CodeNoSubscription),
		api.IsCode(e, api.CodeSubscriptionRequired),
		e.Status == http.StatusPaymentRequired:
		return ExitLimit
	case api.IsCode(e, api.CodeAccessDenied),
		e.Status == http.StatusUnauthorized,
		e.Status == http.StatusForbidden:
		return ExitAuth
	default:
		return ExitFailure
	}
}

// jsonError is the shape of an error in machine mode, mirroring the API's own
// envelope so one parser handles both.
type jsonError struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jsonErrorEnvelope struct {
	Error jsonError `json:"error"`
}

// app is the state every command shares: the loaded configuration, the resolved
// credential, the API client, and the terminal facts (TTY-ness, width) that
// decide between a TUI and plain output.
//
// It is built once per run in PersistentPreRunE. Only PersistentPreRunE
// mutates it, so a command can read these fields without locking.
type app struct {
	cfg    *config.Config
	creds  *config.Credentials
	client *api.Client

	// token and source are what ResolveToken found; apiURL is the effective
	// base URL, after the --api-url flag, which outranks the environment, and
	// apiURLFlag is where pflag stores that flag until init resolves it.
	token      string
	source     config.CredentialSource
	apiURL     string
	apiURLFlag string

	// jsonMode, noInput and isTTY are the global flags plus the terminal this
	// process was started from.
	jsonMode bool
	noInput  bool
	isTTY    bool

	// width and height are the terminal's, with width capped at maxWidth so a
	// wide window does not stretch a report into an unreadable line. Both fall
	// back to sane defaults when there is no terminal.
	width  int
	height int

	// host is the machine name, sent to the API as part of the login client
	// name so the key list in the dashboard says which laptop approved it.
	host string

	// usageCmd is the command whose usage text should follow a usage error. It
	// is recorded where the failure is detected (a flag parse, an argument
	// check) because cobra does not pass the command to the error handler.
	usageCmd *cobra.Command

	// homeNotice is the last home action's failure, carried into the next menu
	// draw. The menu runs on the alternate screen, so a line printed to stderr
	// before it is wiped by the redraw: without this the user never learns that
	// the upload they asked for failed.
	homeNotice string
}

// Command builds the root command and returns it with the error handler main
// wires into fang.
//
// The handler is returned rather than set on the command because it needs the
// resolved app: only the CLI knows how to render an *api.APIError with the
// right next action.
func Command() (*cobra.Command, fang.ErrorHandler) {
	a := &app{
		width:  defaultWidth,
		height: defaultHeight,
		// Cobra parses --json after this, but a usage error before that point
		// still has to be reportable in the mode the user asked for.
		jsonMode: argsWantJSON(os.Args[1:]),
	}

	root := &cobra.Command{
		Use:   "prepublish",
		Short: "Audit a YouTube script before you record it",
		Long: "prepublish scores a draft before it is recorded: hook, structure and pacing,\n" +
			"an attention-risk curve, prioritized rewrites, a title rewrite, an\n" +
			"authenticity check and a YouTube policy pre-flight.\n\n" +
			"It is free without an account (three audits a day, an email required) and\n" +
			"full-strength with one (fifty a day, complete rewrites, video and audio\n" +
			"uploads, thumbnails).",
		Example: "  prepublish audit script.md --title \"Why the Roman concrete lasts\"\n" +
			"  cat script.md | prepublish audit - --title \"...\"\n" +
			"  prepublish hook --text \"Everyone gets this wrong.\" --niche history\n" +
			"  prepublish login\n" +
			"  prepublish runtime script.md --minutes 10",
		Args:              a.rootArgs(),
		RunE:              func(cmd *cobra.Command, _ []string) error { return a.home(ctxOf(cmd), cmd) },
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return a.init() },
		SilenceErrors:     true,
		SilenceUsage:      true,
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return a.usage(cmd, err) })
	// fang owns the version flag; this template keeps its output to the version
	// string itself so `prepublish --version` is scriptable.
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().BoolVar(&a.jsonMode, "json", a.jsonMode,
		"print JSON for scripts; never prompts or opens a TUI")
	root.PersistentFlags().StringVar(&a.apiURLFlag, "api-url", "",
		"base URL of the Prepublish API (overrides $PREPUBLISH_API_URL and the config file)")
	root.PersistentFlags().BoolVar(&a.noInput, "no-input", false,
		"never prompt; fail instead")

	root.AddCommand(
		a.auditCmd(),
		a.reportCmd(),
		a.historyCmd(),
		a.hookCmd(),
		a.policyCmd(),
		a.authenticityCmd(),
		a.loginCmd(),
		a.logoutCmd(),
		a.whoamiCmd(),
		a.runtimeCmd(),
		a.upgradeCmd(),
		a.billingCmd(),
		a.configCmd(),
		a.versionCmd(),
	)

	return root, a.handleError
}

// ctxOf is a guard for the handful of places a command's context is read
// outside RunE; a command always has one by then.
func ctxOf(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// rootArgs accepts no positional arguments and turns anything else into the
// same message cobra would produce, so `prepublish bogus` exits 2 with usage
// instead of pretending the word was a script.
func (a *app) rootArgs() cobra.PositionalArgs {
	return a.args(cobra.NoArgs)
}

// args wraps a cobra argument validator so a violation is a usage error (exit
// 2) that knows which command's usage to print.
func (a *app) args(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		return a.usage(cmd, fn(cmd, args))
	}
}

// usage records the failing command and returns the error as a usage failure.
// It is only for genuine command-line syntax errors — a flag cobra could not
// parse, an argument count that does not fit — where the usage text is the
// answer. A missing value that a flag could have supplied uses usageErr
// instead, so the user is not shown a flag list they already understood.
func (a *app) usage(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	if cmd != nil {
		a.usageCmd = cmd
	}
	return usageErr(err)
}

// init resolves everything a command needs from the flags, environment and
// config files. It runs before every command, including the ones that only
// print local text, so `version` and `runtime` work offline.
func (a *app) init() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a.cfg = cfg

	if err := a.setServiceURLs(); err != nil {
		return err
	}

	creds, credsErr := config.LoadCredentials()
	if credsErr != nil {
		// Not fatal: every anonymous command still works, and a corrupt file
		// should not make `prepublish runtime` fail.
		if path, pathErr := config.CredentialsPath(); pathErr == nil {
			a.warn(ui.Warn("ignoring " + path + ": " + credsErr.Error()))
		} else {
			a.warn(ui.Warn("ignoring the credentials file: " + credsErr.Error()))
		}
	}
	a.creds = creds

	// The stored key is only attached to the API it was issued for, so a
	// --api-url (or a $PREPUBLISH_API_URL from a project's environment) decides
	// where commands talk without also deciding where the key is sent. A key that
	// does not belong here is not used at all, and the user is told why rather
	// than being left to wonder why the command is suddenly anonymous.
	token, source, mismatch := config.ResolveTokenFor(a.apiURL)
	a.token, a.source = token, source
	if mismatch != "" {
		a.warn(ui.Warn(mismatch))
	}
	a.client = api.New(a.apiURL, a.token, version.UserAgent())

	a.isTTY = stdoutIsTTY()
	if w, h, ok := terminalSize(); ok {
		a.width, a.height = w, h
	}
	if a.width > maxWidth {
		a.width = maxWidth
	}
	if a.width <= 0 {
		a.width = defaultWidth
	}
	if a.height <= 0 {
		a.height = defaultHeight
	}
	a.host = hostName()

	// Rendered reports carry their own permalink, and it has to point at the
	// same host `--open` does, including a local app URL.
	ui.SetAppURL(a.cfg.AppURL())

	return nil
}

// setServiceURLs resolves the two base URLs — flag, then environment, then file,
// then the built-in default — and vets whichever value won.
//
// The vetting is a veto, not a preference: a plain-http API URL would put the
// account's key on the wire in the clear, and a plain-http app URL would open an
// approval page that cannot be trusted with a code. Both stop the command here,
// where the message can name the flag, the variable or the file that set it.
//
// The flag outranks the environment and the file, so it is resolved here rather
// than pushed into the config: config.APIURL() prefers the environment by
// design, and the flag must win over that.
func (a *app) setServiceURLs() error {
	flag := strings.TrimSpace(a.apiURLFlag)
	// Trimmed for the same reason config.APIURL() trims: a variable set to
	// whitespace is unset, not a source.
	envAPI := strings.TrimSpace(os.Getenv(config.EnvAPIURL))
	envApp := strings.TrimSpace(os.Getenv(config.EnvAppURL))

	a.apiURL = a.cfg.APIURL()
	switch {
	case flag != "":
		a.apiURL = strings.TrimRight(flag, "/")
		if err := config.ValidateServiceURL(a.apiURL); err != nil {
			return usageErr(fmt.Errorf("--api-url: %w", err))
		}
	case envAPI != "":
		if err := config.ValidateServiceURL(a.apiURL); err != nil {
			return usageErr(fmt.Errorf("$%s: %w", config.EnvAPIURL, err))
		}
	default:
		if err := config.ValidateServiceURL(a.apiURL); err != nil {
			return fmt.Errorf("config.json api_url: %w (set a usable one with `prepublish config set api-url <url>`)", err)
		}
	}

	appURL, appFrom := a.cfg.AppURL(), "config.json app_url"
	if envApp != "" {
		appFrom = "$" + config.EnvAppURL
	}
	if err := config.ValidateServiceURL(appURL); err != nil {
		if envApp != "" {
			return usageErr(fmt.Errorf("%s: %w", appFrom, err))
		}
		return fmt.Errorf("%s: %w (set a usable one with `prepublish config set app-url <url>`)", appFrom, err)
	}
	return nil
}

// interactive reports whether a command may prompt or start a full-screen
// program: a terminal on both ends, no --no-input, and no --json.
//
// It is deliberately stricter than "stdout is a TTY". A TUI on a terminal whose
// stdin is a pipe cannot read a keystroke, and a prompt in machine mode would
// corrupt the JSON a script is parsing.
func (a *app) interactive() bool {
	return a.isTTY && stdinIsTTY() && !a.noInput && !a.jsonMode
}

// signedIn reports whether a credential was resolved, from the file or the
// environment. It is not a promise that the key still works: only the API can
// answer that, which is what Me is for.
func (a *app) signedIn() bool { return a.token != "" }

// anonymousClient is a client with no credential, for the routes that mint one
// (the device flow) and for the anonymous quota read.
func (a *app) anonymousClient() *api.Client {
	return api.New(a.apiURL, "", version.UserAgent())
}

// callerPaid reports whether the signed-in account has the paid features an
// audit checks for. An anonymous caller is never paid, and an unreachable API
// answers "no" so the command asks for the email instead of failing before it
// has tried to send anything.
func (a *app) callerPaid(ctx context.Context) bool {
	if !a.signedIn() {
		return false
	}
	usage, err := a.client.Usage(ctx)
	if err != nil {
		return false
	}
	return usage.Tier == api.TierPaid || usage.Tier == api.TierStudio
}

// adopt makes a freshly saved credential the app's own, so the rest of this run
// (claiming audits, the home loop) acts as the account that just signed in.
func (a *app) adopt(creds *config.Credentials) {
	a.creds = creds
	a.token = creds.APIKey
	a.source = config.SourceFile
	a.client = api.New(a.apiURL, creds.APIKey, version.UserAgent())
}

// credsEmail is the address remembered from the last login, when there is one.
func (a *app) credsEmail() string {
	if a.creds == nil {
		return ""
	}
	return a.creds.Email
}

// home is the bare `prepublish` command: the dashboard for a terminal, and help
// when there is no terminal to drive.
//
// It loops. Every action returns to the menu, so checking a hook after an audit
// does not mean starting the program again, and only Quit (or Ctrl-C) leaves.
// Failures inside an action are printed and the menu comes back: a rejected
// upload should not end a session.
func (a *app) home(ctx context.Context, cmd *cobra.Command) error {
	if !a.interactive() {
		return cmd.Help()
	}

	for {
		state, err := a.homeState(ctx)
		if err != nil {
			return err
		}
		if a.homeNotice != "" {
			state.Notice = a.homeNotice
		}
		choice, err := tui.RunHome(state)
		if err != nil {
			return err
		}

		switch choice {
		case tui.HomeQuit:
			return nil
		case tui.HomeAudit:
			err = a.homeAudit(ctx, cmd)
		case tui.HomeHook:
			err = a.homeTool(ctx, cmd, toolHook)
		case tui.HomePolicy:
			err = a.homeTool(ctx, cmd, toolPolicy)
		case tui.HomeAuthenticity:
			err = a.homeTool(ctx, cmd, toolAuthenticity)
		case tui.HomeHistory:
			err = a.runHistory(ctx, 1)
		case tui.HomeLogin:
			err = a.login(ctx, cmd, loginOptions{})
		case tui.HomeLogout:
			err = a.logout(ctx)
		case tui.HomeUpgrade:
			err = a.upgrade()
		}

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled) {
				continue
			}
			// The menu has no command line to correct, so a usage failure here
			// is only its message. It is printed for the scrollback and carried
			// into the menu, which draws over anything already on screen.
			a.usageCmd = nil
			a.renderError(os.Stderr, err)
			a.homeNotice = noticeLine(err)
			continue
		}
		a.homeNotice = ""
	}
}

// homeState gathers what the menu shows. Identity, plan and quota are all
// best-effort: a menu that only appears when the network works would be the
// worst possible failure mode, so a failure becomes a notice line instead.
func (a *app) homeState(ctx context.Context) (tui.HomeState, error) {
	state := tui.HomeState{
		Width:   a.width,
		APIURL:  a.apiURL,
		Version: version.String(),
		Source:  a.source,
	}

	if !a.signedIn() {
		anonID, err := a.cfg.EnsureAnonymousID()
		if err != nil {
			state.Notice = err.Error()
			return state, nil
		}
		state.Email = a.cfg.Email()
		free, err := a.client.CheckFree(ctx, anonID)
		if err != nil {
			state.Notice = a.identityNotice(err)
			return state, ctx.Err()
		}
		state.Free = free
		return state, ctx.Err()
	}

	user, err := a.client.Me(ctx)
	if err != nil {
		state.Notice = a.identityNotice(err)
		return state, ctx.Err()
	}
	state.User = user
	state.Email = firstNonEmpty(user.Email, a.credsEmail(), a.cfg.Email())
	if usage, err := a.client.Usage(ctx); err == nil {
		state.Usage = usage
	}
	return state, ctx.Err()
}

// noticeLine reduces an error to one short line for the menu's notice row,
// which has no room for a paragraph and cannot scroll.
func noticeLine(err error) string {
	line := err.Error()
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	const limit = 140
	if runes := []rune(line); len(runes) > limit {
		line = string(runes[:limit-1]) + "…"
	}
	return line
}

// identityNotice turns an identity call's failure into one line a menu can
// show, with the action that fixes it.
func (a *app) identityNotice(err error) string {
	switch {
	case api.StatusOf(err) == http.StatusUnauthorized:
		return "the saved API key was rejected — run `prepublish login`"
	case api.IsCode(err, api.CodeNetworkError):
		return "could not reach " + a.apiURL + " — showing local state"
	default:
		return err.Error()
	}
}

// homeAudit asks for the two things an audit cannot guess, then runs the same
// code path as `prepublish audit`.
func (a *app) homeAudit(ctx context.Context, cmd *cobra.Command) error {
	path, err := tui.PromptScriptPath()
	if err != nil {
		return err
	}
	title, err := tui.PromptTitle()
	if err != nil {
		return err
	}
	return a.runAudit(ctx, cmd, auditOptions{path: path, title: title})
}

// homeTool asks for a script path and runs one of the three standalone tools,
// the same way the matching subcommand does.
func (a *app) homeTool(ctx context.Context, cmd *cobra.Command, kind toolKind) error {
	path, err := tui.PromptScriptPath()
	if err != nil {
		return err
	}
	opts := toolOptions{path: path}
	if kind == toolAuthenticity {
		title, err := tui.PromptTitle()
		if err != nil {
			return err
		}
		opts.title = title
	}
	return a.runTool(ctx, cmd, kind, opts)
}

// handleError renders whatever a command returned. fang calls it once per run
// with the writer it has already made color-profile aware.
func (a *app) handleError(w io.Writer, _ fang.Styles, err error) {
	a.renderError(w, err)
}

// renderError is the single place an error becomes text.
//
// Machine mode prints the API's envelope shape so a script parses failures the
// same way it parses responses. Text mode hands every failure to ui's error
// renderer, so a missing flag reads in the same voice as a rejected API call;
// usage text is appended only for the failures that are actually about the
// command line, because a bad argument is the one case where the fix is to read
// the usage.
func (a *app) renderError(w io.Writer, err error) {
	if err == nil {
		return
	}

	var reported *ExitError
	if errors.As(err, &reported) && reported.Reported {
		// The command printed the id and the next step itself; only the exit
		// code is missing.
		return
	}

	if a.jsonMode {
		envelope := jsonErrorEnvelope{Error: jsonError{Message: err.Error()}}
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			envelope.Error.Status = apiErr.Status
			envelope.Error.Code = apiErr.Code
			if apiErr.Message != "" {
				envelope.Error.Message = apiErr.Message
			}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(envelope)
		return
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, huh.ErrUserAborted) {
		fmt.Fprintln(w, ui.Warn("cancelled"))
		return
	}

	fmt.Fprintln(w, ui.RenderErrorWidth(err, a.width))
	if a.usageCmd != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, strings.TrimRight(a.usageCmd.UsageString(), "\n"))
	}
}
