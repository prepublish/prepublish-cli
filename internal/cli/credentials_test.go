package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
)

// swallowOutput silences everything a command writes, so a test that drives one
// does not print warnings and account cards into the suite's output. It returns
// what was written to stderr, which is where warnings go.
func swallowOutput(t *testing.T) func() string {
	t.Helper()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = write, write

	var captured strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		body, _ := io.ReadAll(read)
		captured.Write(body)
	}()

	return func() string {
		os.Stdout, os.Stderr = originalOut, originalErr
		write.Close()
		<-done
		read.Close()
		return captured.String()
	}
}

// TestInitWithholdsAKeyBoundToAnotherAPI is the fix for the worst case in the
// review: one command with --api-url pointing somewhere else used to send the
// production key to that host, and a project's .envrc could do it silently.
func TestInitWithholdsAKeyBoundToAnotherAPI(t *testing.T) {
	freshConfig(t)
	if err := config.SaveCredentials(&config.Credentials{
		APIKey:    "pp_live_hosted",
		APIURL:    config.DefaultAPIURL,
		Source:    config.LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	t.Run("matching API", func(t *testing.T) {
		a := &app{}
		if err := a.init(); err != nil {
			t.Fatalf("init: %v", err)
		}
		if a.token != "pp_live_hosted" || a.source != config.SourceFile {
			t.Errorf("token = %q, source = %q, want the stored key", a.token, a.source)
		}
		if !a.signedIn() {
			t.Error("signedIn() = false with the key attached")
		}
	})

	t.Run("another API", func(t *testing.T) {
		restore := swallowOutput(t)
		a := &app{apiURLFlag: "https://staging.example.com"}
		err := a.init()
		warnings := restore()
		if err != nil {
			t.Fatalf("init: %v", err)
		}

		if a.token != "" || a.source != config.SourceNone {
			t.Errorf("token = %q, source = %q, want no credential for another API", a.token, a.source)
		}
		if a.signedIn() {
			t.Error("signedIn() = true after the key was withheld")
		}
		// Silence would leave the user wondering why they are anonymous again.
		if !strings.Contains(warnings, "stored credentials are for") {
			t.Errorf("stderr = %q, want a one-line warning", warnings)
		}
		if !strings.Contains(warnings, "https://staging.example.com") {
			t.Errorf("stderr = %q, want the API the command actually used", warnings)
		}
	})
}

// TestInitUsesTheEnvironmentKeyAnywhere: exporting the key is an explicit pairing
// of that key with whatever API this command is pointed at, which is what makes
// the CLI usable against staging in CI.
func TestInitUsesTheEnvironmentKeyAnywhere(t *testing.T) {
	freshConfig(t)
	t.Setenv(config.EnvAPIKey, "pp_live_ci")

	a := &app{apiURLFlag: "https://staging.example.com"}
	if err := a.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if a.token != "pp_live_ci" || a.source != config.SourceEnv {
		t.Errorf("token = %q, source = %q, want the environment key", a.token, a.source)
	}
}

// revokeRecorder stands in for the API's revoke route.
type revokeRecorder struct {
	requests []string
	status   int
}

func (r *revokeRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests = append(r.requests, req.Header.Get("Authorization"))
		if r.status != 0 {
			w.WriteHeader(r.status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestRevokeReplacedRetiresTheStoredKey(t *testing.T) {
	freshConfig(t)

	newApp := func(source config.CredentialSource, creds *config.Credentials) *app {
		return &app{
			cfg:    &config.Config{},
			creds:  creds,
			source: source,
			apiURL: "https://api.example.com",
			client: api.New("https://api.example.com", "", ""),
		}
	}
	old := &config.Credentials{APIKey: "pp_live_old", APIURL: "https://api.example.com"}

	t.Run("same key is left alone", func(t *testing.T) {
		recorder := &revokeRecorder{}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceFile, old)
		a.apiURL = srv.URL
		a.revokeReplaced(context.Background(), "pp_live_old")
		if len(recorder.requests) != 0 {
			t.Errorf("revoked %v, want nothing: it is the same key", recorder.requests)
		}
	})

	t.Run("environment key is left alone", func(t *testing.T) {
		recorder := &revokeRecorder{}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceEnv, old)
		a.apiURL = srv.URL
		a.revokeReplaced(context.Background(), "pp_live_new")
		// The key was exported by whoever runs this shell; a CI job that never
		// asked for it must not lose its credential because a human signed in.
		if len(recorder.requests) != 0 {
			t.Errorf("revoked %v, want nothing for an environment credential", recorder.requests)
		}
	})

	t.Run("key bound elsewhere is not sent here", func(t *testing.T) {
		recorder := &revokeRecorder{}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceFile, old)
		a.apiURL = srv.URL // a different origin from the key's
		a.revokeReplaced(context.Background(), "pp_live_new")
		if len(recorder.requests) != 0 {
			t.Errorf("sent the old key to %s: %v", srv.URL, recorder.requests)
		}
	})

	t.Run("revokes the old key once", func(t *testing.T) {
		recorder := &revokeRecorder{}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceFile, old)
		a.apiURL = srv.URL
		a.creds = &config.Credentials{APIKey: "pp_live_old", APIURL: srv.URL}
		restore := swallowOutput(t)
		a.revokeReplaced(context.Background(), "pp_live_new")
		restore()

		if len(recorder.requests) != 1 {
			t.Fatalf("requests = %v, want one", recorder.requests)
		}
		if recorder.requests[0] != "Bearer pp_live_old" {
			t.Errorf("Authorization = %q, want the key being replaced", recorder.requests[0])
		}
	})

	t.Run("an already revoked key is not an error", func(t *testing.T) {
		recorder := &revokeRecorder{status: http.StatusUnauthorized}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceFile, old)
		a.apiURL, a.creds = srv.URL, &config.Credentials{APIKey: "pp_live_old", APIURL: srv.URL}
		restore := swallowOutput(t)
		a.revokeReplaced(context.Background(), "pp_live_new")
		warnings := restore()

		if len(recorder.requests) != 1 {
			t.Errorf("requests = %v, want one attempt and no retry", recorder.requests)
		}
		// The goal is reached, so there is nothing to tell the user.
		if strings.Contains(warnings, "could not revoke") {
			t.Errorf("stderr = %q, want no warning for a key that is already gone", warnings)
		}
	})

	t.Run("a key that cannot be revoked is reported", func(t *testing.T) {
		// The server refuses: the new sign-in must still complete, but the user
		// has to be told the old key is still live.
		recorder := &revokeRecorder{status: http.StatusInternalServerError}
		srv := httptest.NewServer(recorder.handler())
		defer srv.Close()

		a := newApp(config.SourceFile, old)
		a.apiURL, a.creds = srv.URL, &config.Credentials{APIKey: "pp_live_old", APIURL: srv.URL}
		restore := swallowOutput(t)
		a.revokeReplaced(context.Background(), "pp_live_new")
		warnings := restore()

		if len(recorder.requests) != 1 {
			t.Errorf("requests = %v, want one attempt", recorder.requests)
		}
		if !strings.Contains(warnings, "could not revoke the previous key") {
			t.Errorf("stderr = %q, want a warning naming the leftover key", warnings)
		}
		if !strings.Contains(warnings, "/dashboard") {
			t.Errorf("stderr = %q, want the next action", warnings)
		}
	})
}

// TestFinishLoginBindsTheKeyToTheAPI covers the other end of the binding: the
// host is recorded when the key is stored, so the next command can tell whether
// the key belongs to the API it is about to talk to.
func TestFinishLoginBindsTheKeyToTheAPI(t *testing.T) {
	freshConfig(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			io.WriteString(w, `{"user":{"id":"u-1","email":"creator@example.com","subscription_status":"active","analysis_count":1,"created_at":"2026-01-01T00:00:00Z"}}`)
		case "/api/user/usage":
			io.WriteString(w, `{"tier":"paid","audits_used_today":0,"audits_limit":50,"audits_remaining":50}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a := &app{
		cfg:    cfg,
		apiURL: srv.URL,
		client: api.New(srv.URL, "", ""),
		source: config.SourceNone,
	}

	restore := swallowOutput(t)
	err = a.finishLogin(context.Background(), &config.Credentials{
		APIKey: "pp_live_new",
		Source: config.LoginSourceBrowser,
	}, nil)
	restore()
	if err != nil {
		t.Fatalf("finishLogin: %v", err)
	}

	stored, err := config.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if stored == nil {
		t.Fatal("no credentials were stored")
	}
	if stored.APIURL != srv.URL {
		t.Errorf("api_url = %q, want the API that issued the key (%q)", stored.APIURL, srv.URL)
	}

	// And the stored key is usable against that API and withheld from any other.
	if token, source, _ := config.ResolveTokenFor(srv.URL); token != "pp_live_new" || source != config.SourceFile {
		t.Errorf("ResolveTokenFor(own API) = (%q, %q)", token, source)
	}
	if token, _, mismatch := config.ResolveTokenFor("https://attacker.example"); token != "" || mismatch == "" {
		t.Error("the freshly stored key was offered to an unrelated API")
	}
}

// TestLogoutWithAKeyBoundElsewhereDoesNotClaimSignedOut: the stored key is not in
// use against this API, so nothing is revoked — but saying "Not signed in" over a
// file that still holds a live key would be a lie the user cannot see through.
func TestLogoutWithAKeyBoundElsewhereDoesNotClaimSignedOut(t *testing.T) {
	dir := freshConfig(t)
	if err := config.SaveCredentials(&config.Credentials{
		APIKey:    "pp_live_local",
		APIURL:    "http://127.0.0.1:8080",
		Source:    config.LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// What init() produces when the stored key does not belong to the API in
	// effect: no credential, but the file is still there.
	a := &app{
		cfg:    cfg,
		creds:  &config.Credentials{APIKey: "pp_live_local", APIURL: "http://127.0.0.1:8080"},
		source: config.SourceNone,
		apiURL: config.DefaultAPIURL,
		client: api.New(config.DefaultAPIURL, "", ""),
	}

	restore := swallowOutput(t)
	err = a.logout(context.Background())
	said := restore()
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !strings.Contains(said, "127.0.0.1:8080") {
		t.Errorf("output = %q, want the API the stored key belongs to", said)
	}
	if !strings.Contains(said, "logout --api-url") {
		t.Errorf("output = %q, want the command that revokes it", said)
	}

	// The key is untouched: nothing here is allowed to revoke a credential that
	// belongs to another API.
	if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
		t.Errorf("credentials.json was deleted: %v", err)
	}
}
