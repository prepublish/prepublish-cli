package version

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// resolve is the whole of the fallback: the stamps are what -ldflags wrote, and
// the build info is what the toolchain recorded. Both are inputs here, so every
// case is exercised without building anything.

// vcsInfo is a stand-in for debug.ReadBuildInfo: the main module's version plus
// the vcs.* settings the toolchain embeds in a build made in a checkout.
func vcsInfo(mainVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}, Settings: settings}
}

// The revision a checkout embeds is the full 40-character hash, not the short
// one the CLI reports.
const testRevision = "ea07c796c36a44a2b9f6ee34d1a8c3f0d5b7a1e2"

func vcsSettings() []debug.BuildSetting {
	return []debug.BuildSetting{
		{Key: "vcs.revision", Value: testRevision},
		{Key: "vcs.time", Value: "2026-09-29T17:01:14Z"},
		{Key: "vcs.modified", Value: "false"},
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		// The three variables as -ldflags left them; a blank field means the
		// packaged default, which is what a build with no stamps has.
		stampVersion, stampCommit, stampDate string
		info                                 *debug.BuildInfo
		wantVersion, wantCommit, wantDate    string
	}{
		{
			name:         "stamped release build",
			stampVersion: "0.1.0", stampCommit: "ea07c79", stampDate: "2026-09-29T17:01:14Z",
			info:        vcsInfo("v0.1.0", vcsSettings()...),
			wantVersion: "0.1.0", wantCommit: "ea07c79", wantDate: "2026-09-29T17:01:14Z",
		},
		{
			name:         "stamps win over build info",
			stampVersion: "v1.2.3", stampCommit: "abc1234", stampDate: "2026-01-02T03:04:05Z",
			info:        vcsInfo("v9.9.9", vcsSettings()...),
			wantVersion: "1.2.3", wantCommit: "abc1234", wantDate: "2026-01-02T03:04:05Z",
		},
		{
			name:        "empty stamps fall back to build info",
			info:        vcsInfo("v0.1.0", vcsSettings()...),
			wantVersion: "0.1.0", wantCommit: "ea07c79", wantDate: "2026-09-29T17:01:14Z",
		},
		{
			name:         "sha of the whole revision is shortened",
			stampVersion: "0.1.0",
			info:         vcsInfo("(devel)", vcsSettings()...),
			wantVersion:  "0.1.0", wantCommit: "ea07c79", wantDate: "2026-09-29T17:01:14Z",
		},
		{
			name:        "tagged module version",
			info:        vcsInfo("v0.1.0"),
			wantVersion: "0.1.0", wantCommit: "none", wantDate: "unknown",
		},
		{
			name:        "tagged module version with vcs settings",
			info:        vcsInfo("v0.1.0", vcsSettings()...),
			wantVersion: "0.1.0", wantCommit: "ea07c79", wantDate: "2026-09-29T17:01:14Z",
		},
		{
			name:        "pseudo-version carries the commit",
			info:        vcsInfo("v0.0.0-20260929170114-ea07c796c36a"),
			wantVersion: "0.0.0-20260929170114-ea07c796c36a", wantCommit: "ea07c79", wantDate: "unknown",
		},
		{
			name:        "pseudo-version with a suffix",
			info:        vcsInfo("v2.1.4-0.20260929170114-ea07c796c36a+incompatible"),
			wantVersion: "2.1.4-0.20260929170114-ea07c796c36a+incompatible", wantCommit: "ea07c79", wantDate: "unknown",
		},
		{
			name:        "pre-release tag is not a pseudo-version",
			info:        vcsInfo("v1.0.0-rc1"),
			wantVersion: "1.0.0-rc1", wantCommit: "none", wantDate: "unknown",
		},
		{
			name:        "devel with vcs settings",
			info:        vcsInfo("(devel)", vcsSettings()...),
			wantVersion: "dev", wantCommit: "ea07c79", wantDate: "2026-09-29T17:01:14Z",
		},
		{
			name:        "devel with nothing else",
			info:        vcsInfo("(devel)"),
			wantVersion: "dev", wantCommit: "none", wantDate: "unknown",
		},
		{
			name:        "no build info at all",
			info:        nil,
			wantVersion: "dev", wantCommit: "none", wantDate: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stampVersion := tt.stampVersion
			if stampVersion == "" {
				stampVersion = devVersion
			}
			stampCommit := tt.stampCommit
			if stampCommit == "" {
				stampCommit = noneCommit
			}
			stampDate := tt.stampDate
			if stampDate == "" {
				stampDate = unknownDate
			}

			version, commit, date := resolve(stampVersion, stampCommit, stampDate, tt.info)

			if version != tt.wantVersion {
				t.Errorf("version = %q, want %q", version, tt.wantVersion)
			}
			if commit != tt.wantCommit {
				t.Errorf("commit = %q, want %q", commit, tt.wantCommit)
			}
			if date != tt.wantDate {
				t.Errorf("date = %q, want %q", date, tt.wantDate)
			}
		})
	}
}

func TestStringNamesTheBuild(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = originalVersion, originalCommit })

	tests := []struct {
		name, version, commit, want string
	}{
		{"a released build", "0.1.0", "ea07c79", "0.1.0 (ea07c79)"},
		{"a build with no commit", "dev", noneCommit, "dev"},
		{"an empty commit", "dev", "", "dev"},
		{"a tag that kept its v", "v0.1.0", "ea07c79", "0.1.0 (ea07c79)"},
		{"a build stamped with nothing", "", "", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version, Commit = tt.version, tt.commit
			if got := String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserAgentNamesTheClientPlatform(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	platform := "(" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	for _, tt := range []struct{ name, version, wantPrefix string }{
		{"a released build", "0.1.0", "prepublish-cli/0.1.0 "},
		{"a tag that kept its v", "v0.1.0", "prepublish-cli/0.1.0 "},
		// The header is logged and read server-side, so it must never be empty
		// even in a build that was stamped with nothing.
		{"a build stamped with nothing", "", "prepublish-cli/dev "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			got := UserAgent()
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("UserAgent() = %q, want a %q prefix", got, tt.wantPrefix)
			}
			if !strings.Contains(got, platform) {
				t.Errorf("UserAgent() = %q, want %q in it", got, platform)
			}
			if strings.Contains(got, "v0.1.0") {
				t.Errorf("UserAgent() = %q, want the tag without its v", got)
			}
		})
	}
}
