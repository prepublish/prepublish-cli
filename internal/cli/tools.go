package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/ui"
)

// toolKind names the three standalone free tools. They differ only in what they
// ask the API for and which renderer draws the answer, so one flow serves all
// three.
type toolKind int

const (
	toolHook toolKind = iota
	toolPolicy
	toolAuthenticity
)

// toolOptions is the input every tool command accepts.
type toolOptions struct {
	path  string
	text  string
	title string
	niche string
	email string
}

func (a *app) hookCmd() *cobra.Command {
	opts := &toolOptions{}
	cmd := &cobra.Command{
		Use:   "hook [FILE|-]",
		Short: "Score a hook and get rewrites",
		Long: "Score the first seconds of a script: per-sentence attention pull, the\n" +
			"curiosity gap, the distance to the payoff, and rewrites in several styles.\n\n" +
			"Free without an account, with a smaller daily allowance than an audit; the\n" +
			"email raises it.",
		Example: "  prepublish hook --text \"Everyone gets this wrong.\" --niche history\n" +
			"  prepublish hook hook.md --email me@example.com",
		Args: a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := *opts
			if len(args) == 1 {
				o.path = args[0]
			}
			return a.runTool(ctxOf(cmd), cmd, toolHook, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.text, "text", "", "the hook text itself, instead of a file")
	f.StringVar(&opts.niche, "niche", "", "content niche, for example history or finance")
	f.StringVar(&opts.email, "email", "", "email; raises the anonymous daily allowance")
	return cmd
}

func (a *app) policyCmd() *cobra.Command {
	opts := &toolOptions{}
	cmd := &cobra.Command{
		Use:   "policy [FILE|-]",
		Short: "Pre-flight a script against YouTube's policies",
		Long: "Check a script against YouTube's advertiser-friendly and community\n" +
			"guidelines before you record it, and see which passages a reviewer would\n" +
			"look at and why.\n\n" +
			"This is a pre-flight, not a verdict: it reports how strongly the script\n" +
			"matches the published rubric, and the output says so itself.",
		Example: "  prepublish policy script.md --title \"Why the Roman concrete lasts\"",
		Args:    a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := *opts
			if len(args) == 1 {
				o.path = args[0]
			}
			return a.runTool(ctxOf(cmd), cmd, toolPolicy, o)
		},
	}
	cmd.Flags().StringVar(&opts.title, "title", "", "video title, for title-level policy checks")
	return cmd
}

func (a *app) authenticityCmd() *cobra.Command {
	opts := &toolOptions{}
	cmd := &cobra.Command{
		Use:   "authenticity [FILE|-]",
		Short: "Check the inauthentic-content risk",
		Long: "Check a draft against YouTube's inauthentic and reused-content rules: the\n" +
			"Partner Program requirement that a channel's content be original, and the\n" +
			"signals a reviewer would look for.\n\n" +
			"A full audit carries the same check inside its report.",
		Example: "  prepublish authenticity script.md --title \"Why the Roman concrete lasts\"",
		Args:    a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := *opts
			if len(args) == 1 {
				o.path = args[0]
			}
			return a.runTool(ctxOf(cmd), cmd, toolAuthenticity, o)
		},
	}
	cmd.Flags().StringVar(&opts.title, "title", "", "video title (required)")
	return cmd
}

// runTool sends one tool request and prints what came back.
func (a *app) runTool(ctx context.Context, cmd *cobra.Command, kind toolKind, o toolOptions) error {
	// The title is asked for before the script is read: cancelling at a prompt
	// should not cost a file read.
	var title string
	if kind == toolAuthenticity {
		resolved, err := a.resolveTitle(cmd, o.title)
		if err != nil {
			return err
		}
		title = resolved
	}

	text, err := a.toolText(cmd, o)
	if err != nil {
		return err
	}

	switch kind {
	case toolHook:
		// The hook tool is the one place an email is optional: it is passed
		// when one is already known, and never prompted for, because the tool
		// works without it (with a smaller allowance).
		result, err := a.client.HookAnalyze(ctx, text, strings.TrimSpace(o.niche),
			pickEmail(o.email, a.cfg.Email(), a.credsEmail()))
		if err != nil {
			return err
		}
		return a.showToolResult(result, ui.RenderHook(result, a.width))
	case toolPolicy:
		result, err := a.client.PolicyPreflight(ctx, strings.TrimSpace(o.title), text)
		if err != nil {
			return err
		}
		return a.showToolResult(result, ui.RenderPolicy(result, a.width))
	case toolAuthenticity:
		result, err := a.client.AuthenticityCheck(ctx, title, text)
		if err != nil {
			return err
		}
		return a.showToolResult(result, ui.RenderAuthenticity(result, a.width))
	default:
		return fmt.Errorf("unknown tool")
	}
}

// showToolResult prints a tool's answer: the API struct in machine mode, the
// rendered text otherwise.
func (a *app) showToolResult(result any, rendered string) error {
	if a.jsonMode {
		return a.printJSON(result)
	}
	a.result(rendered)
	return nil
}

// toolText resolves the text a tool works on: --text, then a file argument or
// stdin, then a prompted path in a terminal.
func (a *app) toolText(cmd *cobra.Command, o toolOptions) (string, error) {
	if text := strings.TrimSpace(o.text); text != "" {
		return text, nil
	}

	path, err := a.scriptPath(cmd, o.path)
	if err != nil {
		return "", err
	}
	if isMediaFile(path) {
		return "", usageErr(fmt.Errorf(
			"%s is a video or audio file: these tools take a script, not a recording", path))
	}

	text, err := readScript(path, os.Stdin)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", usageErr(fmt.Errorf("%s is empty", describe(path)))
	}
	return text, nil
}
