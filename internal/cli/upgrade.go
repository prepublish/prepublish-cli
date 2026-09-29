package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/ui"
)

func (a *app) upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Open the pricing page",
		Long: "Open the pricing page in a browser.\n\n" +
			"A subscription raises the allowance from three audits a day to fifty and\n" +
			"unlocks the parts of the report the free tier gates: the full rewrites, the\n" +
			"title rewrite, policy passages, video and audio uploads, and thumbnails.",
		Example: "  prepublish upgrade",
		Args:    a.args(cobra.NoArgs),
		RunE:    func(cmd *cobra.Command, _ []string) error { return a.upgrade() },
	}
}

func (a *app) upgrade() error {
	url := a.cfg.AppURL() + "/pricing"
	if a.jsonMode {
		if err := a.printJSON(map[string]string{"url": url}); err != nil {
			return err
		}
		a.openBrowser(url)
		return nil
	}
	a.say(ui.Info("Creator $29/mo or $290/yr · Studio $99/mo or $990/yr"))
	a.say(ui.Info(url))
	a.openBrowser(url)
	return nil
}

func (a *app) billingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "billing",
		Short: "Open the billing portal",
		Long: "Open the subscription's billing portal, where a plan is changed, cancelled\n" +
			"or its invoices read. The URL is minted per account by the API, so this\n" +
			"needs a signed-in account and nothing else.",
		Example: "  prepublish billing",
		Args:    a.args(cobra.NoArgs),
		RunE:    func(cmd *cobra.Command, _ []string) error { return a.billing(ctxOf(cmd)) },
	}
}

func (a *app) billing(ctx context.Context) error {
	if !a.signedIn() {
		return exitErr(ExitAuth, errors.New("billing needs an account: run `prepublish login`"))
	}
	url, err := a.client.BillingPortalURL(ctx)
	if err != nil {
		return err
	}
	if url == "" {
		return errors.New("the API returned no billing URL")
	}
	if a.jsonMode {
		if err := a.printJSON(map[string]string{"url": url}); err != nil {
			return err
		}
		a.openBrowser(url)
		return nil
	}
	a.say(ui.Info(url))
	a.openBrowser(url)
	return nil
}
