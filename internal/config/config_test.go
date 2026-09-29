package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolate points the package at a fresh directory and clears every override a
// developer's shell might be carrying. Without the clearing, a machine with
// PREPUBLISH_API_URL exported would fail the default-URL assertions.
//
// The directory itself does not exist yet: the CLI creates its own config
// directory, and that creation is what sets its mode.
func isolate(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "prepublish")
	t.Setenv(EnvConfigDir, dir)
	t.Setenv(EnvAPIURL, "")
	t.Setenv(EnvAppURL, "")
	t.Setenv(EnvAPIKey, "")
	return dir
}

func TestEnvOverridesFileAndDefault(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.APIURL(); got != DefaultAPIURL {
		t.Errorf("empty config: APIURL = %q, want %q", got, DefaultAPIURL)
	}
	if got := cfg.AppURL(); got != DefaultAppURL {
		t.Errorf("empty config: AppURL = %q, want %q", got, DefaultAppURL)
	}

	// A pinned file value wins over the default, and loses to the environment.
	cfg.SetAPIURL("https://staging.example.com/")
	cfg.SetAppURL("https://staging-app.example.com")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if got := reloaded.APIURL(); got != "https://staging.example.com" {
		t.Errorf("file value: APIURL = %q, want the trailing slash trimmed", got)
	}

	t.Setenv(EnvAPIURL, "http://localhost:8080/")
	t.Setenv(EnvAppURL, "http://localhost:3000")
	if got := reloaded.APIURL(); got != "http://localhost:8080" {
		t.Errorf("env override: APIURL = %q, want %q", got, "http://localhost:8080")
	}
	if got := reloaded.AppURL(); got != "http://localhost:3000" {
		t.Errorf("env override: AppURL = %q, want %q", got, "http://localhost:3000")
	}
}

func TestEnsureAnonymousIDCreatesOnceAndPersists(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AnonymousID() != "" {
		t.Fatalf("fresh config reports anonymous id %q, want empty", cfg.AnonymousID())
	}

	id, err := cfg.EnsureAnonymousID()
	if err != nil {
		t.Fatalf("EnsureAnonymousID: %v", err)
	}
	if !strings.HasPrefix(id, "cli:") {
		t.Errorf("anonymous id = %q, want a cli: prefix", id)
	}
	// The prefix is what the backend maps to OriginCLI; a bare uuid would be
	// counted as web traffic.
	if len(id) != len("cli:")+36 {
		t.Errorf("anonymous id = %q, want cli:<uuid-v4>", id)
	}

	again, err := cfg.EnsureAnonymousID()
	if err != nil {
		t.Fatalf("EnsureAnonymousID (second): %v", err)
	}
	if again != id {
		t.Errorf("second call returned %q, want the stored %q", again, id)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.AnonymousID() != id {
		t.Errorf("reloaded anonymous id = %q, want %q", reloaded.AnonymousID(), id)
	}
	third, err := reloaded.EnsureAnonymousID()
	if err != nil {
		t.Fatalf("EnsureAnonymousID (reloaded): %v", err)
	}
	if third != id {
		t.Errorf("reloaded EnsureAnonymousID = %q, want the persisted %q", third, id)
	}
}

func TestSavedFilesArePrivate(t *testing.T) {
	dir := isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.SetEmail("creator@example.com")
	if _, err := cfg.EnsureAnonymousID(); err != nil {
		t.Fatalf("EnsureAnonymousID: %v", err)
	}
	if err := SaveCredentials(&Credentials{
		APIKey:    "pp_live_secret",
		Source:    LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	configPath, credentialsPath := paths(t)

	if runtime.GOOS != "windows" {
		// Windows has no POSIX mode bits; os.Stat there reports a synthetic
		// 0666 for any writable file, so the assertion would be noise.
		if got := modeOf(t, dir); got != 0o700 {
			t.Errorf("config dir mode = %o, want 0700", got)
		}
		if got := modeOf(t, configPath); got != 0o600 {
			t.Errorf("%s mode = %o, want 0600", configPath, got)
		}
		if got := modeOf(t, credentialsPath); got != 0o600 {
			t.Errorf("%s mode = %o, want 0600", credentialsPath, got)
		}
	}

	// The key must not be readable from the settings file.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	if strings.Contains(string(raw), "pp_live_secret") {
		t.Errorf("config.json contains the API key: %s", raw)
	}
	if !strings.Contains(string(raw), "creator@example.com") {
		t.Errorf("config.json did not persist the email: %s", raw)
	}
}

func TestCredentialsRoundTripAndDelete(t *testing.T) {
	isolate(t)

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials on a fresh dir: %v", err)
	}
	if creds != nil {
		t.Fatalf("LoadCredentials returned %+v, want nil when not signed in", creds)
	}

	want := &Credentials{
		APIKey:    "pp_live_abc",
		KeyID:     "key-1",
		KeyPrefix: "pp_live_abc",
		APIURL:    "https://api.example.com",
		Email:     "creator@example.com",
		Source:    LoginSourceBrowser,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err := SaveCredentials(want); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	got, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got == nil {
		t.Fatal("LoadCredentials returned nil after a save")
	}
	if *got != *want {
		t.Errorf("round trip = %+v, want %+v", *got, *want)
	}

	if err := DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}
	credentialsPath := mustPath(t, CredentialsPath)
	if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
		t.Errorf("credentials file still exists: %v", err)
	}
	// Logging out twice, or when nothing was ever stored, is not a failure.
	if err := DeleteCredentials(); err != nil {
		t.Errorf("second DeleteCredentials: %v", err)
	}
}

func TestResolveTokenSourcePrecedence(t *testing.T) {
	isolate(t)

	if token, source := ResolveToken(); token != "" || source != SourceNone {
		t.Errorf("anonymous: ResolveToken = (%q, %q), want empty and SourceNone", token, source)
	}

	if err := SaveCredentials(&Credentials{
		APIKey:    "pp_live_file",
		Source:    LoginSourceToken,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if token, source := ResolveToken(); token != "pp_live_file" || source != SourceFile {
		t.Errorf("file: ResolveToken = (%q, %q), want the file key and SourceFile", token, source)
	}

	// The environment wins: it is how a CI job or a single command acts as a
	// different account without editing the stored one.
	t.Setenv(EnvAPIKey, "  pp_live_env  ")
	if token, source := ResolveToken(); token != "pp_live_env" || source != SourceEnv {
		t.Errorf("env: ResolveToken = (%q, %q), want the trimmed env key and SourceEnv", token, source)
	}
}

func TestLoadRejectsMalformedConfig(t *testing.T) {
	dir := isolate(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(mustPath(t, Path), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a malformed config; a silent fallback would hide a hand-edit that broke the file")
	}
}

func TestCredentialsFileWithoutKeyReadsAsSignedOut(t *testing.T) {
	dir := isolate(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The residue of an interrupted write: a file exists but holds no key. It
	// must not be reported as a signed-in account with an empty bearer.
	if err := os.WriteFile(mustPath(t, CredentialsPath), []byte(`{"source":"browser"}`), 0o600); err != nil {
		t.Fatalf("write keyless credentials: %v", err)
	}
	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds != nil {
		t.Errorf("LoadCredentials returned %+v, want nil", creds)
	}
	if token, source := ResolveToken(); token != "" || source != SourceNone {
		t.Errorf("ResolveToken = (%q, %q), want empty and SourceNone", token, source)
	}
}

func TestDirHonoursConfigDirEnv(t *testing.T) {
	dir := isolate(t)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != dir {
		t.Errorf("Dir() = %q, want %q", got, dir)
	}
	configPath, credentialsPath := paths(t)
	if configPath != filepath.Join(dir, configFileName) {
		t.Errorf("Path() = %q, want it inside %q", configPath, dir)
	}
	if credentialsPath != filepath.Join(dir, credentialsFileName) {
		t.Errorf("CredentialsPath() = %q, want it inside %q", credentialsPath, dir)
	}
}

// TestDirRequiresAHomeDirectory pins the refusal to guess. The old fallback was
// os.TempDir()/prepublish, a predictable path any other user of the machine can
// create first — and whoever owns the directory can put a credentials.json in it.
func TestDirRequiresAHomeDirectory(t *testing.T) {
	t.Setenv(EnvConfigDir, "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	if runtime.GOOS == "windows" {
		t.Skip("os.UserConfigDir reads %AppData% on Windows")
	}
	dir, err := Dir()
	if err == nil {
		t.Fatalf("Dir() = %q with no home directory; want an error naming $%s", dir, EnvConfigDir)
	}
	if !strings.Contains(err.Error(), EnvConfigDir) {
		t.Errorf("error %q should name $%s", err, EnvConfigDir)
	}
	if _, err := Path(); err == nil {
		t.Error("Path() succeeded without a resolvable config directory")
	}
	if _, err := LoadCredentials(); err == nil {
		t.Error("LoadCredentials() succeeded without a resolvable config directory")
	}
}

// TestDirRefusesADirectoryThisUserDoesNotOwn: whoever owns the config directory
// can replace credentials.json and config.json inside it, which is enough to
// point the CLI at another API and hand it a key.
func TestDirRefusesADirectoryThisUserDoesNotOwn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ownership is not reported the same way on Windows")
	}
	dir := isolate(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	original := dirOwner
	t.Cleanup(func() { dirOwner = original })

	// The directory this test made belongs to the user running it, so the rule
	// has to be driven with the answer a directory owned by someone else gives.
	dirOwner = func(fs.FileInfo) (int, bool) { return os.Geteuid() + 1, true }
	_, err := Dir()
	if err == nil {
		t.Fatal("Dir accepted a directory owned by another user")
	}
	if !strings.Contains(err.Error(), EnvConfigDir) {
		t.Errorf("error %q should say how to point the CLI somewhere else", err)
	}

	dirOwner = func(fs.FileInfo) (int, bool) { return os.Geteuid(), true }
	if _, err := Dir(); err != nil {
		t.Fatalf("Dir rejected the user's own directory: %v", err)
	}
}

// TestWriteRefusesAFileInPlaceOfTheDirectory covers the other half of the check:
// a path that exists but is not a directory cannot hold the CLI's files.
func TestWriteRefusesAFileInPlaceOfTheDirectory(t *testing.T) {
	parent := t.TempDir()
	notADir := filepath.Join(parent, "config")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(EnvConfigDir, notADir)

	if _, err := Dir(); err == nil {
		t.Fatal("Dir accepted a regular file as the config directory")
	}
	if err := SaveCredentials(&Credentials{APIKey: "pp_live_x"}); err == nil {
		t.Fatal("SaveCredentials wrote under a path that is not a directory")
	}
}

// paths resolves the two files, failing the test when they cannot be resolved.
func paths(t *testing.T) (configPath, credentialsPath string) {
	t.Helper()
	return mustPath(t, Path), mustPath(t, CredentialsPath)
}

// mustPath resolves a path getter, failing the test on error.
func mustPath(t *testing.T, get func() (string, error)) string {
	t.Helper()
	path, err := get()
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	return path
}

// modeOf reports a path's permission bits.
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
