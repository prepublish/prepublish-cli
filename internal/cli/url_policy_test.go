package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prepublish/prepublish-cli/internal/config"
)

// freshConfig points the config package at an empty directory and clears the
// overrides a developer's shell might be carrying.
func freshConfig(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "prepublish")
	t.Setenv(config.EnvConfigDir, dir)
	t.Setenv(config.EnvAPIURL, "")
	t.Setenv(config.EnvAppURL, "")
	t.Setenv(config.EnvAPIKey, "")
	return dir
}

// TestInitRefusesPlainHTTPForTheAPI is the one that matters most: what the flag,
// the environment and the file all feed into is the address an API key is sent
// to, and http would put it on the wire in the clear.
func TestInitRefusesPlainHTTPForTheAPI(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		freshConfig(t)
		a := &app{apiURLFlag: "http://api.example.com"}
		err := a.init()
		if err == nil {
			t.Fatal("init accepted a plain-http --api-url")
		}
		if ExitCode(err) != ExitUsage {
			t.Errorf("exit code = %d, want %d for a bad flag", ExitCode(err), ExitUsage)
		}
		if !strings.Contains(err.Error(), "--api-url") {
			t.Errorf("error %q should name the flag", err)
		}
		// The message has to say what to do, not just what is wrong.
		if !strings.Contains(err.Error(), "https") {
			t.Errorf("error %q should say to use https", err)
		}
	})

	t.Run("environment", func(t *testing.T) {
		freshConfig(t)
		t.Setenv(config.EnvAPIURL, "http://api.example.com")
		a := &app{}
		err := a.init()
		if err == nil {
			t.Fatal("init accepted a plain-http $PREPUBLISH_API_URL")
		}
		if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), config.EnvAPIURL) {
			t.Errorf("error = %v (exit %d), want a usage error naming $%s", err, ExitCode(err), config.EnvAPIURL)
		}
	})

	t.Run("config file", func(t *testing.T) {
		freshConfig(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		// Set without validation, which is what a hand-edited file looks like.
		cfg.SetAPIURL("http://api.example.com")
		if err := cfg.Save(); err != nil {
			t.Fatalf("config.Save: %v", err)
		}

		a := &app{}
		err = a.init()
		if err == nil {
			t.Fatal("init accepted a plain-http api_url from the config file")
		}
		if !strings.Contains(err.Error(), "config.json") {
			t.Errorf("error %q should name the file that set it", err)
		}
		// A hand-edit is not a command-line mistake, so it is not a usage error.
		if ExitCode(err) != ExitFailure {
			t.Errorf("exit code = %d, want %d", ExitCode(err), ExitFailure)
		}
	})

	t.Run("loopback is allowed", func(t *testing.T) {
		freshConfig(t)
		a := &app{apiURLFlag: "http://localhost:8080/"}
		if err := a.init(); err != nil {
			t.Fatalf("init refused a loopback API URL: %v", err)
		}
		if a.apiURL != "http://localhost:8080" {
			t.Errorf("apiURL = %q", a.apiURL)
		}
	})
}

func TestInitRefusesPlainHTTPForTheAppURL(t *testing.T) {
	freshConfig(t)
	t.Setenv(config.EnvAppURL, "http://prepublish.example.com")

	a := &app{}
	err := a.init()
	if err == nil {
		t.Fatal("init accepted a plain-http $PREPUBLISH_APP_URL")
	}
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), config.EnvAppURL) {
		t.Errorf("error = %v (exit %d), want a usage error naming $%s", err, ExitCode(err), config.EnvAppURL)
	}
}

// TestConfigSetRefusesPlainHTTP keeps the bad value out of the file in the first
// place, which is where it would otherwise be picked up by every later command —
// including the ones that send a key.
func TestConfigSetRefusesPlainHTTP(t *testing.T) {
	dir := freshConfig(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	a := &app{cfg: cfg, jsonMode: true}
	for _, key := range []string{"api-url", "app-url"} {
		t.Run(key, func(t *testing.T) {
			cmd := a.configSetCmd()
			cmd.SetArgs([]string{key, "http://api.example.com"})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("config set %s accepted a plain-http URL", key)
			}
			if ExitCode(err) != ExitUsage {
				t.Errorf("exit code = %d, want %d", ExitCode(err), ExitUsage)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Errorf("config.json was written despite the rejected value: %v", err)
	}

	// The loopback exception still works, which is what a local app needs.
	cmd := a.configSetCmd()
	cmd.SetArgs([]string{"api-url", "http://127.0.0.1:8080"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set api-url refused a loopback address: %v", err)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if reloaded.APIURL() != "http://127.0.0.1:8080" {
		t.Errorf("stored api_url = %q, want the loopback address", reloaded.APIURL())
	}
}
