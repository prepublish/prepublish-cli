package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prepublish/prepublish-cli/internal/config"
)

func TestIsMediaFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"interview.mp4", true},
		{"INTERVIEW.MKV", true},
		{"/tmp/audio/take-3.flac", true},
		{"voice memo.m4a", true},
		{"script.md", false},
		{"script.mp4.txt", false},
		{"-", false},
		{"", false},
		{"script", false},
		// A directory that happens to look like a recording is not one; the
		// upload path reports the read failure with the real reason.
		{"episode.mp4/script.md", false},
	}
	for _, tc := range cases {
		if got := isMediaFile(tc.path); got != tc.want {
			t.Errorf("isMediaFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestPickEmailPrecedence pins the order the CLI looks for an address in: the
// flag, then the config file, then the last login. Getting this wrong means an
// audit sends to the wrong mailbox.
func TestPickEmailPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		flag  string
		cfg   string
		creds string
		want  string
	}{
		{"flag wins", "flag@example.com", "cfg@example.com", "creds@example.com", "flag@example.com"},
		{"config beats credentials", "", "cfg@example.com", "creds@example.com", "cfg@example.com"},
		{"credentials last resort", "", "", "creds@example.com", "creds@example.com"},
		{"nothing", "", "", "", ""},
		{"whitespace is not a value", "  ", " ", "", ""},
		{"trimmed", "  flag@example.com  ", "", "", "flag@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickEmail(tc.flag, tc.cfg, tc.creds); got != tc.want {
				t.Fatalf("pickEmail(%q, %q, %q) = %q, want %q", tc.flag, tc.cfg, tc.creds, got, tc.want)
			}
		})
	}
}

func TestEmailForUsesSavedAddress(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.SetEmail("saved@example.com")

	a := &app{cfg: cfg, noInput: true, creds: &config.Credentials{Email: "login@example.com"}}

	got, err := a.emailFor(nil, "")
	if err != nil {
		t.Fatalf("emailFor: %v", err)
	}
	if got != "saved@example.com" {
		t.Fatalf("emailFor = %q, want the saved address", got)
	}

	got, err = a.emailFor(nil, "flag@example.com")
	if err != nil {
		t.Fatalf("emailFor with flag: %v", err)
	}
	if got != "flag@example.com" {
		t.Fatalf("emailFor = %q, want the flag", got)
	}
}

// TestEmailForWithoutTerminalIsUsageError is the scripting contract: a
// non-interactive free audit with no address anywhere fails fast with a hint
// instead of hanging on a prompt.
func TestEmailForWithoutTerminalIsUsageError(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a := &app{cfg: cfg, noInput: true}

	_, err = a.emailFor(nil, "")
	if err == nil {
		t.Fatal("emailFor returned no error without an address and no terminal")
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit code = %d, want %d", ExitCode(err), ExitUsage)
	}
	if !strings.Contains(err.Error(), "--email") {
		t.Fatalf("error %q should name the flag that fixes it", err.Error())
	}
}

func TestReadScript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.md")
	if err := os.WriteFile(path, []byte("hello there\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readScript(path, strings.NewReader("ignored"))
	if err != nil {
		t.Fatalf("readScript(file): %v", err)
	}
	if got != "hello there\n" {
		t.Fatalf("readScript(file) = %q", got)
	}

	got, err = readScript("-", strings.NewReader("from stdin"))
	if err != nil {
		t.Fatalf("readScript(-): %v", err)
	}
	if got != "from stdin" {
		t.Fatalf("readScript(-) = %q", got)
	}

	if _, err := readScript(filepath.Join(dir, "missing.md"), strings.NewReader("")); err == nil {
		t.Fatal("readScript(missing) returned no error")
	} else if !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("error %q should name the missing file", err.Error())
	}
}

func TestTallerThanScreen(t *testing.T) {
	short := "one\ntwo\nthree"
	if tallerThanScreen(short, 24) {
		t.Fatal("three lines should fit a 24-line terminal")
	}
	tall := strings.Repeat("line\n", 40)
	if !tallerThanScreen(tall, 24) {
		t.Fatal("40 lines should not fit a 24-line terminal")
	}
	// No measured terminal height still has to decide something sane.
	if !tallerThanScreen(tall, 0) {
		t.Fatal("40 lines should not fit when the height is unknown")
	}
}

func TestArgsWantJSON(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"audit", "script.md"}, false},
		{[]string{"audit", "--json"}, true},
		{[]string{"--json=true", "whoami"}, true},
		{[]string{"--json", "--json=false"}, false},
		{[]string{"--json=false", "--json"}, true},
	}
	for _, tc := range cases {
		if got := argsWantJSON(tc.args); got != tc.want {
			t.Errorf("argsWantJSON(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
