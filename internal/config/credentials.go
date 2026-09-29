package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const credentialsFileName = "credentials.json"

// CredentialSource says where the key in use came from. It is deliberately not
// the same thing as LoginSource: a key pasted into $PREPUBLISH_API_KEY was
// obtained somehow, but nothing in this file has to exist for it to work.
type CredentialSource string

// Where ResolveToken found the key.
const (
	// SourceNone means no key is available: the CLI will run anonymously.
	SourceNone CredentialSource = ""
	// SourceEnv is $PREPUBLISH_API_KEY.
	SourceEnv CredentialSource = "env"
	// SourceFile is credentials.json.
	SourceFile CredentialSource = "file"
)

// LoginSource records how a stored key was obtained, so `whoami` and `logout`
// can be honest about it: a browser login left an account-named key in the
// dashboard list, a pasted token was created by the user somewhere else and
// should not be revoked from here without asking.
type LoginSource string

// How the stored key was obtained.
const (
	LoginSourceBrowser LoginSource = "browser"
	LoginSourceToken   LoginSource = "token"
)

// Credentials is the account state stored in credentials.json.
type Credentials struct {
	// APIKey is the secret. It is the only secret the CLI holds.
	APIKey string `json:"api_key"`
	// KeyID and KeyPrefix identify the key server-side; KeyID is what a logout
	// would revoke and KeyPrefix is what the dashboard shows.
	KeyID     string `json:"key_id,omitempty"`
	KeyPrefix string `json:"key_prefix,omitempty"`
	// APIURL is the API the key was issued for. The key is only ever sent back
	// to that origin: `--api-url` and $PREPUBLISH_API_URL decide where a command
	// talks, and without this they would also decide where the key goes.
	APIURL string `json:"api_url,omitempty"`
	// Email is the account's address, remembered so free-tool calls and the
	// account card do not need a round trip.
	Email string `json:"email,omitempty"`
	// Source is how this key was obtained.
	Source LoginSource `json:"source"`
	// CreatedAt is when the CLI stored it, not when the server minted it.
	CreatedAt time.Time `json:"created_at"`
}

// BoundURL is the API this key belongs to. Credentials written before the CLI
// recorded one are treated as the hosted API, which is the only API that existed
// when they were saved.
func (c *Credentials) BoundURL() string {
	if c == nil || strings.TrimSpace(c.APIURL) == "" {
		return DefaultAPIURL
	}
	return c.APIURL
}

// CredentialsPath is the credentials file's full path.
func CredentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

// LoadCredentials reads credentials.json. A missing file returns (nil, nil):
// "not signed in" is a normal state, not a failure, and callers that only want
// to know whether a key exists should not have to tell the two apart.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		// A file with no key is the residue of an interrupted write and would
		// otherwise be reported as a signed-in account with an empty bearer.
		return nil, nil
	}
	return &creds, nil
}

// SaveCredentials writes credentials.json atomically at mode 0600, creating
// the directory at 0700 on first use.
func SaveCredentials(creds *Credentials) error {
	if creds == nil {
		return errors.New("config: refusing to save nil credentials")
	}
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic(path, data, filePerm)
}

// DeleteCredentials removes credentials.json. It is idempotent: logging out
// twice, or when $PREPUBLISH_API_KEY is the only key, is not an error.
func DeleteCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	return removeFile(path)
}

// ResolveToken returns the key to send to the hosted API and where it came from.
//
// A caller that has an effective API URL — which is every command, since
// --api-url and $PREPUBLISH_API_URL can move it — must use ResolveTokenFor
// instead, or a key bound to one host would be sent to another.
func ResolveToken() (string, CredentialSource) {
	token, source, _ := ResolveTokenFor(DefaultAPIURL)
	return token, source
}

// ResolveTokenFor returns the key to send to apiURL, where it came from, and a
// one-line reason when a stored key was deliberately withheld.
//
// $PREPUBLISH_API_KEY always wins and is always sent: setting it is an explicit
// statement that this key belongs with whatever API this command is pointed at.
// A stored key is different — the user chose the key once, when they signed in —
// so it is sent only to the origin it was issued for. Anything else runs signed
// out, and the reason is returned so the caller can say so rather than leaving
// the user to wonder why they are anonymous again.
//
// Errors are swallowed on purpose: an unreadable credentials file must degrade
// to "anonymous" rather than fail every command.
func ResolveTokenFor(apiURL string) (string, CredentialSource, string) {
	if key := strings.TrimSpace(os.Getenv(EnvAPIKey)); key != "" {
		return key, SourceEnv, ""
	}

	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		return "", SourceNone, ""
	}
	if bound := creds.BoundURL(); !SameOrigin(bound, apiURL) {
		return "", SourceNone, fmt.Sprintf(
			"stored credentials are for %s; running signed out against %s",
			Origin(bound), Origin(apiURL))
	}
	return creds.APIKey, SourceFile, ""
}
