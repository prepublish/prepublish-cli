package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prepublish/prepublish-cli/internal/api"
)

const sampleID = "6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f"

// TestReportRef pins what `prepublish report` accepts. The URL case is the one
// that matters in practice: it is what is on the clipboard after reading a
// report, and rejecting it would send the user back to the browser to find the
// id by hand.
func TestReportRef(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare id", sampleID, sampleID, false},
		{"padded id", "  " + sampleID + "\n", sampleID, false},
		{"uppercase id", strings.ToUpper(sampleID), strings.ToUpper(sampleID), false},
		{"report url", "https://prepublish.ai/analysis/" + sampleID, sampleID, false},
		{"report url with query", "https://prepublish.ai/analysis/" + sampleID + "?utm_source=cli", sampleID, false},
		{"report url with fragment", "https://prepublish.ai/analysis/" + sampleID + "#scores", sampleID, false},
		{"report url with slash", "https://prepublish.ai/analysis/" + sampleID + "/", sampleID, false},
		{"local report url", "http://localhost:3188/analysis/" + sampleID, sampleID, false},
		{"path only", "/analysis/" + sampleID, sampleID, false},
		{"empty", "", "", true},
		{"not an id", "nope", "", true},
		{"short id", sampleID[:8], "", true},
		{"id with a typo", "6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3z", "", true},
		{"url with no id", "https://prepublish.ai/analysis", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reportRef(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("reportRef(%q) = %q, want an error", tc.in, got)
				}
				if ExitCode(usageErr(err)) != ExitUsage {
					t.Fatalf("a bad report reference should be a usage failure, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("reportRef(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("reportRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseTimeout(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 15 * time.Minute, false},
		{"15m", 15 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"1h", time.Hour, false},
		{" 20m ", 20 * time.Minute, false},
		{"0", 0, false},
		{"soon", 0, true},
		{"-5m", 0, true},
		{"15", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseTimeout(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseTimeout(%q) = %v, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTimeout(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseTimeout(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestAuditStillRunningKeepsTheId is the timeout contract: the request already
// succeeded, so the failure has to carry the id and the next step rather than a
// bare "timed out" that leaves the work unreachable.
func TestAuditStillRunningKeepsTheId(t *testing.T) {
	pending := &api.Analysis{ID: sampleID}
	url := "https://prepublish.ai/analysis/" + sampleID

	// Machine mode: the envelope is the only output, so the message carries it.
	a := &app{jsonMode: true}
	err := a.auditStillRunning(pending, url, 15*time.Minute)
	if ExitCode(err) != ExitFailure {
		t.Fatalf("exit code = %d, want %d", ExitCode(err), ExitFailure)
	}
	if !strings.Contains(err.Error(), sampleID) || !strings.Contains(err.Error(), "prepublish report") {
		t.Fatalf("error %q should name the id and the command that picks it up", err.Error())
	}
	var exit *ExitError
	if errors.As(err, &exit) && exit.Reported {
		t.Fatal("machine mode must not mark the error as already reported: the envelope is the report")
	}

	// Text mode: the details are printed, so the handler adds only the code.
	text := &app{}
	err = text.auditStillRunning(pending, url, 15*time.Minute)
	if ExitCode(err) != ExitFailure {
		t.Fatalf("exit code = %d, want %d", ExitCode(err), ExitFailure)
	}
	if !errors.As(err, &exit) || !exit.Reported {
		t.Fatal("text mode already printed the id and the URL, so the error handler should stay quiet")
	}
}
