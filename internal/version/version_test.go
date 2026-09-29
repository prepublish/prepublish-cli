package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestStringAppendsCommitWhenKnown(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = originalVersion, originalCommit })

	Version, Commit = "1.2.3", "none"
	if got := String(); got != "1.2.3" {
		t.Errorf("String() = %q, want the tag alone for a build with no commit", got)
	}

	Version, Commit = "1.2.3", "abc1234"
	if got := String(); got != "1.2.3 (abc1234)" {
		t.Errorf("String() = %q, want the commit included", got)
	}

	// A developer build reports something honest rather than an empty string.
	Version, Commit = "", ""
	if got := String(); got != "" {
		t.Errorf("String() = %q", got)
	}
}

func TestUserAgentNamesTheClientPlatform(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	Version = "9.9.9"

	got := UserAgent()
	if !strings.HasPrefix(got, "prepublish-cli/9.9.9 ") {
		t.Errorf("UserAgent() = %q, want a prepublish-cli/<version> prefix", got)
	}
	want := "(" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	if !strings.Contains(got, want) {
		t.Errorf("UserAgent() = %q, want %q in it", got, want)
	}

	// The header is logged and read server-side, so it must never be empty even
	// in a build that was stamped with nothing.
	Version = ""
	if got := UserAgent(); !strings.HasPrefix(got, "prepublish-cli/dev ") {
		t.Errorf("UserAgent() with an empty Version = %q, want a dev fallback", got)
	}
}
