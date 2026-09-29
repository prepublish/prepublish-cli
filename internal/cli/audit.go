package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/auth"
	"github.com/prepublish/prepublish-cli/internal/tui"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

// defaultAuditTimeout is how long `audit` waits for a report before handing the
// id back. Long enough for a long recording to upload, transcribe and analyze;
// short enough that a broken worker does not hold a terminal hostage.
const defaultAuditTimeout = "15m"

// auditOptions is everything `prepublish audit` can be told. The same struct
// serves the subcommand and the home menu, so the two cannot drift.
type auditOptions struct {
	path      string
	title     string
	email     string
	thumbnail string
	duration  int
	category  string
	audience  string
	timeout   string
	noWait    bool
	open      bool
	pager     bool

	// waitFor is the parsed --timeout. The home menu builds auditOptions itself
	// and has no flag to read, so the empty string means the default.
	waitFor time.Duration
}

func (a *app) auditCmd() *cobra.Command {
	opts := &auditOptions{}
	cmd := &cobra.Command{
		Use:   "audit [FILE|-]",
		Short: "Audit a script and print the report",
		Long: "Audit a draft and get the full report: scores, the attention-risk curve,\n" +
			"prioritized rewrites, a title rewrite, an authenticity check and a policy\n" +
			"pre-flight.\n\n" +
			"FILE is a script, or a video or audio file to transcribe (uploads need a\n" +
			"subscription). `-` reads the script from stdin. With no argument the CLI\n" +
			"asks for a path.\n\n" +
			"Without an account the audit is free, needs an email (--email, or the one\n" +
			"you saved) and returns a gated report.",
		Example: "  prepublish audit script.md --title \"Why the Roman concrete lasts\"\n" +
			"  cat script.md | prepublish audit - --title \"Why the Roman concrete lasts\"\n" +
			"  prepublish audit interview.mp4 --title \"The whole story\"\n" +
			"  prepublish audit script.md --title \"...\" --json --no-wait",
		Args: a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := *opts
			if len(args) == 1 {
				o.path = args[0]
			}
			return a.runAudit(ctxOf(cmd), cmd, o)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.title, "title", "", "video title (required; prompted when omitted in a terminal)")
	f.StringVar(&opts.email, "email", "", "email for the free tier (remembered afterwards)")
	f.StringVar(&opts.thumbnail, "thumbnail", "", "thumbnail image to include (paid)")
	f.IntVar(&opts.duration, "duration", 0, "planned video length in seconds")
	f.StringVar(&opts.category, "category", "", "content category, e.g. history")
	f.StringVar(&opts.audience, "audience", "", "intended audience")
	f.BoolVar(&opts.noWait, "no-wait", false, "queue the audit and exit with its id and URL")
	f.StringVar(&opts.timeout, "timeout", defaultAuditTimeout,
		"give up waiting for the report after this long; 0 waits forever")
	f.BoolVar(&opts.open, "open", false, "open the report in a browser")
	f.BoolVar(&opts.pager, "pager", false, "always open the report in the pager")

	return cmd
}

// runAudit resolves the input, sends it, waits, and prints the report. It is the
// whole audit flow minus the flags, so the home menu can drive it too.
func (a *app) runAudit(ctx context.Context, cmd *cobra.Command, o auditOptions) error {
	path, err := a.scriptPath(cmd, o.path)
	if err != nil {
		return err
	}

	// The timeout is resolved before anything is sent: a typo in --timeout
	// should not cost an upload or spend a daily audit.
	waitFor, err := parseTimeout(o.timeout)
	if err != nil {
		return usageErr(err)
	}
	o.waitFor = waitFor

	// A recording is not a script: it goes up as a file and comes back as a
	// transcript, which is a paid path.
	if isMediaFile(path) {
		return a.runMediaAudit(ctx, cmd, path, o)
	}

	script, err := readScript(path, os.Stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(script) == "" {
		return usageErr(fmt.Errorf("%s is empty: an audit needs a script", describe(path)))
	}

	title, err := a.resolveTitle(cmd, o.title)
	if err != nil {
		return err
	}

	req := api.AnalyzeRequest{
		VideoTitle: title,
		ScriptText: script,
		Category:   strings.TrimSpace(o.category),
		Audience:   strings.TrimSpace(o.audience),
	}
	if o.duration > 0 {
		duration := o.duration
		req.VideoDuration = &duration
	}
	if !a.signedIn() {
		anonID, err := a.cfg.EnsureAnonymousID()
		if err != nil {
			return err
		}
		req.AnonymousUserID = anonID
	}

	// The email is only sent when it is needed, and it is only asked for when
	// the account is known not to be paid. Skipping the Usage round trip for a
	// caller who already has an address on file keeps the free path to two
	// requests.
	email := pickEmail(o.email, a.cfg.Email(), a.credsEmail())
	if email == "" && !a.callerPaid(ctx) {
		email, err = a.emailFor(cmd, "")
		if err != nil {
			return err
		}
	}
	req.Email = email

	return a.analyzeAndRender(ctx, req, o)
}

// runMediaAudit uploads a video or audio file and audits its transcript.
func (a *app) runMediaAudit(ctx context.Context, cmd *cobra.Command, path string, o auditOptions) error {
	if !a.signedIn() {
		return exitErr(ExitAuth, errors.New(
			"video and audio audits need an account: run `prepublish login` (uploads are part of the subscription)"))
	}
	if _, ok := api.UploadContentType(path); !ok {
		return usageErr(fmt.Errorf("%s: the API cannot transcribe %s files; convert it to one of %s",
			path, filepath.Ext(path), strings.Join(api.SupportedUploadExtensions(), ", ")))
	}

	title, err := a.resolveTitle(cmd, o.title)
	if err != nil {
		return err
	}

	upload := func(ctx context.Context, onProgress func(sent, total int64)) (string, error) {
		return a.client.Upload(ctx, path, onProgress)
	}

	var uploadID string
	if a.interactive() {
		uploadID, err = tui.RunUploadProgress(ctx, upload)
	} else {
		a.warn(ui.Info("uploading " + filepath.Base(path)))
		uploadID, err = upload(ctx, a.uploadProgress(filepath.Base(path)))
	}
	if err != nil {
		return err
	}

	req := api.AnalyzeRequest{
		VideoTitle: title,
		UploadID:   uploadID,
		Category:   strings.TrimSpace(o.category),
		Audience:   strings.TrimSpace(o.audience),
	}
	if o.duration > 0 {
		duration := o.duration
		req.VideoDuration = &duration
	}

	return a.analyzeAndRender(ctx, req, o)
}

// analyzeAndRender sends one audit, waits for it, and prints the report.
func (a *app) analyzeAndRender(ctx context.Context, req api.AnalyzeRequest, o auditOptions) error {
	pending, err := a.client.Analyze(ctx, req, strings.TrimSpace(o.thumbnail))
	if err != nil {
		return err
	}

	url := ui.ReportURL(a.cfg.AppURL(), pending.ID)
	if o.noWait {
		if a.jsonMode {
			return a.printJSON(pending)
		}
		a.say(ui.Success("Audit queued"))
		a.say(ui.Info("id:     " + pending.ID))
		a.say(ui.Info("report: " + url))
		return nil
	}

	// The wait gets its own deadline so a worker that never finishes does not
	// hold the terminal: the audit is already accepted and has an id, and the
	// id is worth more than the wait.
	waitCtx := ctx
	if o.waitFor > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, o.waitFor)
		defer cancel()
	}

	wait := func(ctx context.Context, onUpdate func(*api.Analysis)) (*api.Analysis, error) {
		return a.client.WaitAnalysis(ctx, pending.ID, 0, onUpdate)
	}

	var analysis *api.Analysis
	if a.interactive() {
		analysis, err = tui.RunAuditProgress(waitCtx, wait)
	} else {
		analysis, err = wait(waitCtx, a.analysisProgress())
	}
	if err != nil {
		// Our own deadline, not the user's Ctrl-C: the request succeeded and
		// the work continues server-side.
		if ctx.Err() == nil && waitCtx.Err() != nil {
			return a.auditStillRunning(pending, url, o.waitFor)
		}
		return err
	}

	// A failed audit is not a transport error: the report itself is the answer,
	// and its error_message is the server's explanation.
	if analysis.Status == api.StatusFailed {
		message := "the audit failed"
		if analysis.ErrorMessage != nil && strings.TrimSpace(*analysis.ErrorMessage) != "" {
			message = strings.TrimSpace(*analysis.ErrorMessage)
		}
		if a.jsonMode {
			_ = a.printJSON(analysis)
		}
		return exitErr(ExitFailure, errors.New(message))
	}

	if o.open {
		a.openBrowser(url)
	}
	if a.jsonMode {
		return a.printJSON(analysis)
	}
	return a.showReport(analysis, o.pager)
}

// auditStillRunning is the timeout outcome. The audit was accepted and keeps
// working on the server, so the id and the URL are the deliverable: reporting
// only an error would throw away the result of the request that did succeed.
func (a *app) auditStillRunning(pending *api.Analysis, url string, waited time.Duration) error {
	waited = waited.Round(time.Second)
	summary := fmt.Sprintf("the audit is still running after %s: `prepublish report %s` picks it up later",
		waited, pending.ID)
	if a.jsonMode {
		// Nothing has been printed to stdout, so the envelope carries all of it.
		return exitErr(ExitFailure, errors.New(summary))
	}

	a.warn(ui.Info(fmt.Sprintf("still running after %s — the audit keeps working on the server", waited)))
	a.warn(ui.Info("id      " + pending.ID))
	a.warn(ui.Info("report  " + url))
	a.warn(ui.Info("pick it up later with `prepublish report " + pending.ID + "`"))
	return &ExitError{Code: ExitFailure, Err: errors.New(summary), Reported: true}
}

// parseTimeout reads --timeout. An empty value means the home menu built the
// options without a flag, and gets the default; "0" means wait forever.
func parseTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultAuditTimeout
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s is not a timeout: use a duration such as 15m, 90s or 1h, or 0 to wait forever", value)
	}
	return parsed, nil
}

// showReport renders a finished audit: the raw struct in machine mode, the
// pager when the report is taller than the terminal, plain text otherwise.
func (a *app) showReport(analysis *api.Analysis, forcePager bool) error {
	if a.jsonMode {
		return a.printJSON(analysis)
	}
	report := ui.RenderReport(analysis, a.width)
	if a.interactive() && (forcePager || tallerThanScreen(report, a.height)) {
		return tui.RunReportViewer(report)
	}
	a.result(report)
	return nil
}

// scriptPath resolves which script to read: the argument, or a prompted path
// when there is a terminal to ask.
func (a *app) scriptPath(cmd *cobra.Command, arg string) (string, error) {
	if path := strings.TrimSpace(arg); path != "" {
		return path, nil
	}
	if !a.interactive() {
		return "", usageErr(errors.New("pass a script file, or `-` to read it from stdin"))
	}
	path, err := tui.PromptScriptPath()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", usageErr(errors.New("a script file is required"))
	}
	return strings.TrimSpace(path), nil
}

// resolveTitle resolves the video title: the flag, or a prompt when there is a
// terminal. The API rejects an audit without a title, so a non-terminal caller
// has to supply one.
func (a *app) resolveTitle(cmd *cobra.Command, flagValue string) (string, error) {
	if title := strings.TrimSpace(flagValue); title != "" {
		return title, nil
	}
	if !a.interactive() {
		return "", usageErr(errors.New("a video title is required: pass --title"))
	}
	title, err := tui.PromptTitle()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(title) == "" {
		return "", usageErr(errors.New("a video title is required"))
	}
	return strings.TrimSpace(title), nil
}

// emailFor resolves the address a free audit needs and remembers it, so it is
// asked for once per machine rather than once per audit.
func (a *app) emailFor(cmd *cobra.Command, flagEmail string) (string, error) {
	if email := pickEmail(flagEmail, a.cfg.Email(), a.credsEmail()); email != "" {
		return email, nil
	}
	if !a.interactive() {
		return "", usageErr(errors.New(
			"an email is required for a free audit: pass --email, or sign in with `prepublish login`"))
	}

	email, err := tui.PromptEmail("")
	if err != nil {
		return "", err
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return "", usageErr(errors.New("that does not look like an email address"))
	}

	a.cfg.SetEmail(email)
	if err := a.cfg.Save(); err != nil {
		a.warn(ui.Warn("could not save the email: " + err.Error()))
	}
	return email, nil
}

// openBrowser opens a URL, reporting a failure as a warning rather than an
// error: on a headless machine the command still did its real work.
func (a *app) openBrowser(url string) {
	if err := auth.OpenBrowser(url); err != nil {
		a.warn(ui.Warn(err.Error()))
		return
	}
	a.say(ui.Info("opened " + url))
}

// describe names the input in an error message, where stdin has no path.
func describe(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}
