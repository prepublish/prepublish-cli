package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

func (a *app) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "whoami",
		Aliases: []string{"status"},
		Short:   "Show the account you are using, or your free quota",
		Long: "Show who the CLI is acting as: the account, its plan tier, today's audit\n" +
			"quota and whether the key came from the environment or the config file.\n\n" +
			"Signed out, it reports the anonymous allowance for this machine instead.",
		Example: "  prepublish whoami\n" +
			"  prepublish whoami --json",
		Args: a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runWhoami(ctxOf(cmd)) },
	}
}

// whoamiJSON is the machine-readable account card: the two API structs plus the
// one fact only the CLI knows, where the credential came from.
type whoamiJSON struct {
	User   *api.User  `json:"user"`
	Usage  *api.Usage `json:"usage"`
	Source string     `json:"source,omitempty"`
}

func (a *app) runWhoami(ctx context.Context) error {
	if !a.signedIn() {
		anonID, err := a.cfg.EnsureAnonymousID()
		if err != nil {
			return err
		}
		free, err := a.client.CheckFree(ctx, anonID)
		if err != nil {
			return err
		}
		if a.jsonMode {
			return a.printJSON(free)
		}
		a.result(ui.RenderAccount(nil, nil, free, config.SourceNone, a.width))
		return nil
	}

	// Me is the only call that proves the key still works: an invalid key is
	// silently ignored by the API's optional-auth routes, but this one is
	// authenticated.
	user, err := a.client.Me(ctx)
	if err != nil {
		return err
	}
	usage, err := a.client.Usage(ctx)
	if err != nil {
		return err
	}

	if a.jsonMode {
		return a.printJSON(whoamiJSON{User: user, Usage: usage, Source: string(a.source)})
	}
	a.result(ui.RenderAccount(user, usage, nil, a.source, a.width))
	return nil
}
