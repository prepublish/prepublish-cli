package cli

import (
	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/ui"
	"github.com/prepublish/prepublish-cli/internal/version"
)

// versionCmd is the scriptable version of fang's --version flag: it reports the
// same build as one line of text, or as JSON for a bug report or a CI check.
func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and build details",
		Long: "Print the build this binary came from.\n\n" +
			"A release binary is stamped with its tag, commit and build date. `go build`\n" +
			"from a checkout reports dev, and `go install` reports the module version;\n" +
			"either way the commit is what identifies the build.",
		Args: a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.jsonMode {
				return a.printJSON(struct {
					Version   string `json:"version"`
					Commit    string `json:"commit"`
					Date      string `json:"date"`
					UserAgent string `json:"user_agent"`
				}{
					Version:   version.Version,
					Commit:    version.Commit,
					Date:      version.Date,
					UserAgent: version.UserAgent(),
				})
			}
			a.result("prepublish " + version.String())
			a.result(ui.Info("built " + version.Date + " · " + version.UserAgent()))
			return nil
		},
	}
}
