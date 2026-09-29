package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/config"
)

// TestResolveTitleUsesFlag is the common case: a script that passes --title
// never touches a prompt.
func TestResolveTitleUsesFlag(t *testing.T) {
	a := &app{noInput: true}
	got, err := a.resolveTitle(&cobra.Command{Use: "audit"}, "  Why the Roman concrete lasts  ")
	if err != nil {
		t.Fatalf("resolveTitle: %v", err)
	}
	if got != "Why the Roman concrete lasts" {
		t.Fatalf("resolveTitle = %q, want the trimmed flag", got)
	}
}

// TestResolveTitleRequiresFlagWithoutTerminal is what a script hitting a
// missing --title sees: exit 2 and a message naming the flag — but not the
// command's whole usage block, because a missing value is not a syntax error
// and the flag list would bury the one line that matters.
func TestResolveTitleRequiresFlagWithoutTerminal(t *testing.T) {
	a := &app{noInput: true}
	cmd := &cobra.Command{Use: "audit", Short: "Audit a script"}
	cmd.Flags().String("title", "", "video title")

	_, err := a.resolveTitle(cmd, "")
	if err == nil {
		t.Fatal("resolveTitle returned no error without a title and no terminal")
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit code = %d, want %d", ExitCode(err), ExitUsage)
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Fatalf("error %q should name the flag that fixes it", err.Error())
	}
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error %v does not carry an exit code", err)
	}
	if a.usageCmd != nil {
		t.Fatal("a missing title should not drag the usage text along")
	}
}

// TestRunRuntimeMinutesRejectsScript pins the mutually exclusive input: a
// word-count target and a script cannot both be the question.
func TestRunRuntimeMinutesRejectsScript(t *testing.T) {
	a := &app{noInput: true, cfg: &config.Config{}}
	err := a.runRuntime(&cobra.Command{Use: "runtime"}, "script.md", 10)
	if err == nil {
		t.Fatal("runRuntime accepted both a script and --minutes")
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit code = %d, want %d", ExitCode(err), ExitUsage)
	}
}

// TestInitAPIURLPrecedence pins the layer order for the API base URL:
// --api-url, then $PREPUBLISH_API_URL, then the config file, then the hosted
// default. The flag case is the one that has been wrong: resolving the config
// first and then reading the flag variable overwrites the flag with the value
// it was meant to outrank.
func TestInitAPIURLPrecedence(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())

	// No environment, no file: the built-in default.
	t.Setenv(config.EnvAPIURL, "")
	a := &app{}
	if err := a.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if a.apiURL != config.DefaultAPIURL {
		t.Fatalf("apiURL = %q, want the default %q", a.apiURL, config.DefaultAPIURL)
	}

	// The file outranks the default.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.SetAPIURL("https://file.example")
	if err := cfg.Save(); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	a = &app{}
	if err := a.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if a.apiURL != "https://file.example" {
		t.Fatalf("apiURL = %q, want the config file's value", a.apiURL)
	}

	// The environment outranks the file.
	t.Setenv(config.EnvAPIURL, "https://env.example")
	a = &app{}
	if err := a.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if a.apiURL != "https://env.example" {
		t.Fatalf("apiURL = %q, want $%s", a.apiURL, config.EnvAPIURL)
	}

	// The flag outranks both, and a trailing slash is trimmed so joining a path
	// to it cannot produce a double slash. A loopback address is the one plain-http
	// URL the CLI accepts, which is what makes it usable against a local server.
	a = &app{apiURLFlag: "http://localhost:8080/"}
	if err := a.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if a.apiURL != "http://localhost:8080" {
		t.Fatalf("apiURL = %q, want the --api-url flag", a.apiURL)
	}
}
