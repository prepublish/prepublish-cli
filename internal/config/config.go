// Package config owns everything the CLI remembers between runs: where it
// lives, how it is written, and which layer wins when two of them disagree.
//
// Two files, both mode 0600 inside a 0700 directory:
//
//	config.json       settings and the anonymous id — safe to print or copy
//	credentials.json  the API key, plus which login produced it
//
// They are separate on purpose. `prepublish config path` hands out the settings
// file, bug reports quote it, and neither should be able to leak a key.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Defaults for the two service URLs. The CLI talks to the API directly: the web
// app's /backend-api rewrite exists to keep browser cookies on the front end's
// origin, and a CLI has no cookies.
const (
	DefaultAPIURL = "https://api.prepublish.ai"
	DefaultAppURL = "https://prepublish.ai"
)

// Environment variables the CLI reads. ConfigDir moves both files, which is
// what the test suite and a sandboxed install use; the rest override the file.
const (
	EnvConfigDir = "PREPUBLISH_CONFIG_DIR"
	EnvAPIURL    = "PREPUBLISH_API_URL"
	EnvAppURL    = "PREPUBLISH_APP_URL"
	EnvAPIKey    = "PREPUBLISH_API_KEY"
)

const (
	configFileName = "config.json"
	// dirPerm and filePerm are the only modes the CLI writes. The key file is
	// the reason both are tight rather than merely conventional.
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// Config is the settings file plus the layer above it.
//
// The fields are unexported so the accessors are the only way to read a value:
// resolving defaults and environment overrides at the accessor keeps one
// answer to "which API am I talking to", instead of a struct field that
// silently disagrees with the environment it was loaded under.
type Config struct {
	path        string
	apiURL      string
	appURL      string
	email       string
	anonymousID string
}

// configFile is the on-disk shape. It exists so the wire format has one
// definition, independent of how Config is laid out in memory.
type configFile struct {
	APIURL      string `json:"api_url,omitempty"`
	AppURL      string `json:"app_url,omitempty"`
	Email       string `json:"email,omitempty"`
	AnonymousID string `json:"anonymous_id"`
}

// Dir is the directory holding config.json and credentials.json:
// $PREPUBLISH_CONFIG_DIR when set, otherwise <user config dir>/prepublish,
// which is ~/.config/prepublish on Linux and
// ~/Library/Application Support/prepublish on macOS.
//
// It fails rather than guessing. Without a home directory to key off there is no
// directory this process can prove is the user's own, and the old fallback to a
// world-writable /tmp/prepublish was a directory an earlier user of the machine
// could have created first — which is enough to serve the CLI a credentials
// file of their choosing. The error names the variable that fixes it.
//
// A directory that already exists is checked before it is used: it must be a
// directory owned by this user.
func Dir() (string, error) {
	dir := strings.TrimSpace(os.Getenv(EnvConfigDir))
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("cannot find your config directory (%v): set $%s to a directory this account can write to", err, EnvConfigDir)
		}
		dir = filepath.Join(base, "prepublish")
	}
	if err := vetDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// Path is the settings file's full path, for `prepublish config path`.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// Load reads the settings file. A missing file is not an error: it is the
// state of every first run, and the defaults are a working configuration.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	cfg := &Config{path: path}

	raw, err := os.ReadFile(cfg.path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cfg.path, err)
	}

	var f configFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cfg.path, err)
	}
	cfg.apiURL, cfg.appURL, cfg.email, cfg.anonymousID = f.APIURL, f.AppURL, f.Email, f.AnonymousID
	return cfg, nil
}

// Save writes the settings file atomically, creating the directory on first
// use. Only values that differ from the defaults are persisted, so a file that
// exists stays readable and a file that only carries the anonymous id does not
// pretend to pin a URL the user never chose.
func (c *Config) Save() error {
	if c.path == "" {
		path, err := Path()
		if err != nil {
			return err
		}
		c.path = path
	}
	data, err := json.MarshalIndent(c.wire(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic(c.path, data, filePerm)
}

// wire is the on-disk projection of the in-memory config.
func (c *Config) wire() configFile {
	return configFile{
		APIURL:      c.apiURL,
		AppURL:      c.appURL,
		Email:       c.email,
		AnonymousID: c.anonymousID,
	}
}

// APIURL is the base URL for API calls: $PREPUBLISH_API_URL, else the file,
// else the hosted API. The trailing slash is trimmed so joining a path to it
// cannot produce a double slash.
func (c *Config) APIURL() string {
	if v := strings.TrimSpace(os.Getenv(EnvAPIURL)); v != "" {
		return strings.TrimRight(v, "/")
	}
	if c.apiURL != "" {
		return strings.TrimRight(c.apiURL, "/")
	}
	return DefaultAPIURL
}

// SetAPIURL pins the API base URL in the settings file. Pass "" to clear the
// pin and fall back to the environment or the default again.
func (c *Config) SetAPIURL(url string) {
	c.apiURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

// AppURL is the web app's base URL: $PREPUBLISH_APP_URL, else the file, else
// prepublish.ai. Links opened in a browser — device login, a report permalink,
// pricing, the billing portal — are built from it.
func (c *Config) AppURL() string {
	if v := strings.TrimSpace(os.Getenv(EnvAppURL)); v != "" {
		return strings.TrimRight(v, "/")
	}
	if c.appURL != "" {
		return strings.TrimRight(c.appURL, "/")
	}
	return DefaultAppURL
}

// SetAppURL pins the app base URL in the settings file; "" clears the pin.
func (c *Config) SetAppURL(url string) {
	c.appURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

// Email is the address remembered from config, a previous audit or the login
// that stored it. Free audits require one, and the CLI prompts once rather than
// every run.
func (c *Config) Email() string {
	return c.email
}

// SetEmail remembers the address used for free audits.
func (c *Config) SetEmail(email string) {
	c.email = strings.TrimSpace(email)
}

// AnonymousID is the stored anonymous id, empty when the CLI has never run an
// anonymous audit. Prefer EnsureAnonymousID unless the caller only needs to
// read it.
func (c *Config) AnonymousID() string {
	return c.anonymousID
}

// EnsureAnonymousID returns the anonymous id, creating and saving
// `cli:<uuid-v4>` on first use.
//
// The `cli:` prefix is not decoration: the backend derives the analytics origin
// from it (OriginCLI) and counts anonymous usage per id, so audits run before a
// login stay claimable afterwards and do not look like web traffic.
func (c *Config) EnsureAnonymousID() (string, error) {
	if c.anonymousID != "" {
		return c.anonymousID, nil
	}
	c.anonymousID = "cli:" + uuid.NewString()
	if err := c.Save(); err != nil {
		return "", err
	}
	return c.anonymousID, nil
}
