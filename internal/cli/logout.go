package cli

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

func (a *app) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out and revoke the stored API key",
		Long: "Sign out.\n\n" +
			"A key this CLI stored is revoked on the server first, so nothing is left\n" +
			"live in the dashboard, and then credentials.json is deleted. A key that has\n" +
			"already been revoked is not an error, and an API that cannot be reached\n" +
			"still signs you out locally, with a warning naming the dashboard.\n\n" +
			"$PREPUBLISH_API_KEY outranks the stored file, so if it is set this command\n" +
			"only explains that: unset it to sign out of that shell.",
		Example: "  prepublish logout",
		Args:    a.args(cobra.NoArgs),
		RunE:    func(cmd *cobra.Command, _ []string) error { return a.logout(ctxOf(cmd)) },
	}
}

func (a *app) logout(ctx context.Context) error {
	switch a.source {
	case config.SourceNone:
		if a.creds != nil {
			// A stored key that was withheld because it belongs to another API.
			// Nothing was signed out — it is not in use — but the file is still
			// there, and "Not signed in" would leave the user believing a live
			// key had been removed.
			a.say(ui.Warn("the stored key belongs to " + config.Origin(a.creds.BoundURL())))
			a.say(ui.Info("this command is pointed at " + config.Origin(a.apiURL)))
			a.say(ui.Info("run `prepublish logout --api-url " + a.creds.BoundURL() + "` to revoke it"))
			return nil
		}
		a.say(ui.Info("Not signed in"))
		return nil
	case config.SourceEnv:
		a.say(ui.Warn("$PREPUBLISH_API_KEY is set and outranks the stored credentials"))
		a.say(ui.Info("Unset it to sign out of this shell; nothing was deleted"))
		return nil
	}

	// The stored key is revoked before the file is deleted, so the dashboard
	// list does not keep a live key for a machine that no longer uses it. The
	// failures that mean "already gone" are success.
	if err := a.client.RevokeCurrentKey(ctx); err != nil {
		switch api.StatusOf(err) {
		case http.StatusUnauthorized, http.StatusNotFound:
			// Revoked earlier, or never existed. The goal is reached.
		default:
			a.warn(ui.Warn("could not revoke the key: " + err.Error()))
			a.warn(ui.Info("it is still listed at " + a.cfg.AppURL() + "/dashboard"))
		}
	}

	if err := config.DeleteCredentials(); err != nil {
		return err
	}
	a.say(ui.Success("Signed out"))
	return nil
}
