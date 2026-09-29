package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

// configKeys are the settings a user may read and write, in the order the
// listing shows them.
var configKeys = []string{"api-url", "app-url", "email"}

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write the CLI's settings",
		Long: "Read and write config.json, the settings file (credentials live in a\n" +
			"separate file at mode 0600 and are never printed here).\n\n" +
			"Keys: api-url, app-url, email. Environment variables outrank the file:\n" +
			"$PREPUBLISH_API_URL, $PREPUBLISH_APP_URL, and $PREPUBLISH_API_KEY for the\n" +
			"credential, so `config get` reports what is actually in effect and says so\n" +
			"when the environment is the reason.",
		Example: "  prepublish config get api-url\n" +
			"  prepublish config set api-url https://staging-api.example.com\n" +
			"  prepublish config unset email\n" +
			"  prepublish config path",
		Args: a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		a.configGetCmd(),
		a.configSetCmd(),
		a.configUnsetCmd(),
		a.configPathCmd(),
	)
	return cmd
}

// configValues is the effective configuration: what the CLI would use right
// now, after flags, environment and file.
func (a *app) configValues() map[string]string {
	return map[string]string{
		"api-url": a.apiURL,
		"app-url": a.cfg.AppURL(),
		"email":   a.cfg.Email(),
	}
}

func (a *app) configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [KEY]",
		Short: "Print one setting, or all of them",
		Long: "Print the settings in effect, or one of them.\n\n" +
			"Keys: api-url, app-url, email. With no key, all three are listed. What\n" +
			"is printed is what the next command will use, so an environment override\n" +
			"appears here and is called out on stderr.",
		Args: a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			values := a.configValues()

			if len(args) == 1 {
				key := strings.ToLower(strings.TrimSpace(args[0]))
				value, ok := values[key]
				if !ok {
					return usageErr(unknownConfigKey(key))
				}
				a.noteEnvOverride(key)
				if a.jsonMode {
					return a.printJSON(map[string]string{key: value})
				}
				if value == "" {
					a.result("")
					return nil
				}
				a.result(value)
				return nil
			}

			if a.jsonMode {
				return a.printJSON(struct {
					APIURL string `json:"api_url"`
					AppURL string `json:"app_url"`
					Email  string `json:"email"`
				}{APIURL: values["api-url"], AppURL: values["app-url"], Email: values["email"]})
			}

			out := &strings.Builder{}
			table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, key := range configKeys {
				value := values[key]
				if value == "" {
					value = "(not set)"
				}
				fmt.Fprintf(table, "%s\t%s\n", key, value)
			}
			_ = table.Flush()
			a.result(strings.TrimRight(out.String(), "\n"))
			for _, key := range configKeys {
				a.noteEnvOverride(key)
			}
			return nil
		},
	}
}

func (a *app) configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Write one setting",
		Long: "Write one setting to config.json.\n\n" +
			"Keys: api-url, app-url, email. A service URL must be https, or http on a\n" +
			"loopback address, so a typo — or an address that would put the account's key\n" +
			"on the wire in the clear — fails here rather than on the next command. The\n" +
			"API key is not a setting: it lives in credentials.json, which nothing here\n" +
			"reads or prints.",
		Args: a.args(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(strings.TrimSpace(args[0]))
			value := strings.TrimSpace(args[1])

			switch key {
			case "api-url":
				if err := checkURL(value); err != nil {
					return usageErr(err)
				}
				a.cfg.SetAPIURL(value)
			case "app-url":
				if err := checkURL(value); err != nil {
					return usageErr(err)
				}
				a.cfg.SetAppURL(value)
			case "email":
				a.cfg.SetEmail(value)
			default:
				return usageErr(unknownConfigKey(key))
			}

			if err := a.cfg.Save(); err != nil {
				return err
			}
			if !a.jsonMode {
				a.say(ui.Success(key + " set"))
			}
			a.noteEnvOverride(key)
			return nil
		},
	}
}

func (a *app) configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset KEY",
		Short: "Clear one setting",
		Long: "Clear one setting, so the environment or the built-in default applies\n" +
			"again.\n\nKeys: api-url, app-url, email.",
		Args: a.args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(strings.TrimSpace(args[0]))
			switch key {
			case "api-url":
				a.cfg.SetAPIURL("")
			case "app-url":
				a.cfg.SetAppURL("")
			case "email":
				a.cfg.SetEmail("")
			default:
				return usageErr(unknownConfigKey(key))
			}

			if err := a.cfg.Save(); err != nil {
				return err
			}
			if !a.jsonMode {
				a.say(ui.Success(key + " cleared"))
			}
			a.noteEnvOverride(key)
			return nil
		},
	}
}

func (a *app) configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print where the settings and credentials live",
		Args:  a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, dirErr := config.Dir()
			path, pathErr := config.Path()
			creds, credsErr := config.CredentialsPath()
			if err := errors.Join(dirErr, pathErr, credsErr); err != nil {
				return err
			}

			if a.jsonMode {
				return a.printJSON(struct {
					Dir         string `json:"dir"`
					Config      string `json:"config"`
					Credentials string `json:"credentials"`
				}{Dir: dir, Config: path, Credentials: creds})
			}
			a.result(path)
			a.say(ui.Info("credentials: " + creds))
			return nil
		},
	}
}

// noteEnvOverride explains a `get` result that the environment, not the file,
// decided. Without it a user who just ran `config set api-url` and sees the old
// value concludes the write failed.
func (a *app) noteEnvOverride(key string) {
	if a.jsonMode {
		return
	}
	var name string
	switch key {
	case "api-url":
		name = config.EnvAPIURL
	case "app-url":
		name = config.EnvAppURL
	default:
		return
	}
	if os.Getenv(name) == "" {
		return
	}
	a.warn(ui.Info("note: $" + name + " is set and overrides the stored value"))
}

// checkURL rejects a service URL the CLI would not be willing to use, before it
// is written to a file the next command would fail on. The policy itself lives
// with the rest of the base-URL rules, so `config set` and the flag and
// environment paths cannot drift apart.
func checkURL(value string) error {
	return config.ValidateServiceURL(value)
}

func unknownConfigKey(key string) error {
	return fmt.Errorf("unknown setting %q: expected one of %s", key, strings.Join(configKeys, ", "))
}
