package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateServiceURL(t *testing.T) {
	accepted := []string{
		"https://api.prepublish.ai",
		"https://api.prepublish.ai:8443/base",
		"https://staging.example.com/",
		"http://localhost:8080",
		"http://127.0.0.1:3000",
		"http://127.9.9.9",
		"http://[::1]:3000",
		"http://LOCALHOST:3000",
	}
	for _, raw := range accepted {
		if err := ValidateServiceURL(raw); err != nil {
			t.Errorf("ValidateServiceURL(%q) = %v, want nil", raw, err)
		}
	}

	rejected := []string{
		"",
		"   ",
		"api.prepublish.ai",             // no scheme
		"/just/a/path",                  // no host
		"ftp://api.prepublish.ai",       // not a web scheme
		"http://api.prepublish.ai",      // a key over plain http
		"http://staging.example.com",    // not loopback either
		"http://10.0.1.5:8080",          // a private address is not this machine
		"https://",                      // no host
		"https://api.example.com/\nfoo", // a control character in the URL
	}
	for _, raw := range rejected {
		if err := ValidateServiceURL(raw); err == nil {
			t.Errorf("ValidateServiceURL(%q) = nil, want an error", raw)
		}
	}
}

func TestValidateServiceURLExplainsPlainHTTP(t *testing.T) {
	err := ValidateServiceURL("http://api.prepublish.ai")
	if err == nil {
		t.Fatal("plain http to a remote host was accepted")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error %q should say what to use instead", err)
	}
}

func TestOriginCanonicalises(t *testing.T) {
	cases := map[string]string{
		"https://api.prepublish.ai":           "https://api.prepublish.ai",
		"https://api.prepublish.ai:443":       "https://api.prepublish.ai",
		"https://API.Prepublish.AI/dashboard": "https://api.prepublish.ai",
		"http://localhost:3000/x?y=1#f":       "http://localhost:3000",
		"http://127.0.0.1:80":                 "http://127.0.0.1",
		"not a url":                           "",
		"/relative":                           "",
		"":                                    "",
	}
	for raw, want := range cases {
		if got := Origin(raw); got != want {
			t.Errorf("Origin(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	same := [][2]string{
		{"https://api.prepublish.ai", "https://api.prepublish.ai/v1/health"},
		{"https://api.prepublish.ai:443", "https://api.prepublish.ai"},
		{"http://localhost:8080", "http://localhost:8080/backend-api"},
	}
	for _, pair := range same {
		if !SameOrigin(pair[0], pair[1]) {
			t.Errorf("SameOrigin(%q, %q) = false, want true", pair[0], pair[1])
		}
	}

	different := [][2]string{
		// The scheme matters: a key that is safe on https is not safe on http,
		// so those are different origins even for the same host.
		{"https://api.prepublish.ai", "http://api.prepublish.ai"},
		{"https://api.prepublish.ai", "https://attacker.example"},
		{"https://api.prepublish.ai", "https://api.prepublish.ai.attacker.example"},
		{"https://api.prepublish.ai", "https://api.prepublish.ai:8443"},
		{"https://api.prepublish.ai", "https://prepublish.ai"},
		{"", "https://api.prepublish.ai"},
	}
	for _, pair := range different {
		if SameOrigin(pair[0], pair[1]) {
			t.Errorf("SameOrigin(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}

func TestStoredKeyIsOnlySentToTheAPIThatIssuedIt(t *testing.T) {
	isolate(t)

	if err := SaveCredentials(&Credentials{
		APIKey:    "pp_live_hosted",
		APIURL:    "https://api.prepublish.ai",
		Source:    LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// A path, a trailing slash or an explicit default port is the same API.
	for _, target := range []string{
		"https://api.prepublish.ai",
		"https://api.prepublish.ai/",
		"https://api.prepublish.ai:443",
	} {
		token, source, mismatch := ResolveTokenFor(target)
		if token != "pp_live_hosted" || source != SourceFile || mismatch != "" {
			t.Errorf("ResolveTokenFor(%q) = (%q, %q, %q), want the stored key", target, token, source, mismatch)
		}
	}

	// Anywhere else runs signed out, and says so. The key is not sent to a
	// --api-url someone social-engineered, to a typo, or to a project's
	// .envrc-supplied PREPUBLISH_API_URL.
	for _, target := range []string{
		"https://attacker.example",
		"https://api.prepublish.ai.attacker.example",
		"http://api.prepublish.ai", // the same host over plain http is not the same origin
		"https://api.prepublish.ai:8443",
		"https://prepublish.ai",
	} {
		token, source, mismatch := ResolveTokenFor(target)
		if token != "" || source != SourceNone {
			t.Errorf("ResolveTokenFor(%q) = (%q, %q), want no credential", target, token, source)
		}
		if mismatch == "" {
			t.Errorf("ResolveTokenFor(%q) withheld the key without saying why", target)
		}
		if !strings.Contains(mismatch, "https://api.prepublish.ai") {
			t.Errorf("mismatch %q should name the API the key belongs to", mismatch)
		}
	}
}

func TestLegacyCredentialsBelongToTheHostedAPI(t *testing.T) {
	isolate(t)

	// A credentials file written before the CLI recorded an API URL: the hosted
	// API is the only one that existed then, so that is what it is bound to.
	if err := SaveCredentials(&Credentials{
		APIKey:    "pp_live_legacy",
		Source:    LoginSourceToken,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	token, source := ResolveToken()
	if token != "pp_live_legacy" || source != SourceFile {
		t.Errorf("ResolveToken() = (%q, %q), want the legacy key", token, source)
	}
	if got := (&Credentials{APIKey: "x"}).BoundURL(); got != DefaultAPIURL {
		t.Errorf("BoundURL() = %q, want %q", got, DefaultAPIURL)
	}
	if _, source, _ := ResolveTokenFor("https://staging.example.com"); source != SourceNone {
		t.Error("a legacy key was sent to a staging API it was never issued for")
	}
}

func TestEnvironmentKeyIsAlwaysSent(t *testing.T) {
	isolate(t)

	if err := SaveCredentials(&Credentials{
		APIKey:    "pp_live_hosted",
		APIURL:    "https://api.prepublish.ai",
		Source:    LoginSourceBrowser,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// Setting the variable is an explicit pairing of this key with whatever API
	// this command is pointed at, so it is never withheld — that is what makes
	// the CLI usable against a staging API in CI.
	t.Setenv(EnvAPIKey, "pp_live_ci")
	token, source, mismatch := ResolveTokenFor("https://staging.example.com")
	if token != "pp_live_ci" || source != SourceEnv || mismatch != "" {
		t.Errorf("ResolveTokenFor = (%q, %q, %q), want the environment key", token, source, mismatch)
	}
}
