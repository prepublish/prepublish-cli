// Package version carries the build metadata a release stamps into the binary.
//
// Version, Commit and Date are the stamps: a release build writes them with
// -ldflags -X (see the Makefile and .goreleaser.yaml). Whatever a build leaves
// unstamped is filled in before main from the module and VCS metadata the Go
// toolchain records in the binary, so a binary installed with
// `go install github.com/prepublish/prepublish-cli/cmd/prepublish@v0.1.0`
// reports the tag it came from rather than "dev" with nothing else to go on. A
// stamped value always wins; the build info only fills the gaps.
//
// The three variables are resolved once, at init, so every reader — the
// --version flag, the `version` command, the User-Agent — names the same build
// and no call pays for the lookup. They are also normalised there to the same
// shape a release uses: the displayed version carries no leading "v", because
// GoReleaser stamps `{{ .Version }}`, which is the tag minus its "v".
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// The values a build that was stamped with nothing reports, and the sentinels
// resolve uses to tell an unset variable from a stamped one.
const (
	devVersion  = "dev"
	noneCommit  = "none"
	unknownDate = "unknown"
)

// How much of a revision the CLI reports. Seven characters is what
// `git rev-parse --short` and GoReleaser's .ShortCommit print, so a stamped and
// an inferred build name the same commit the same way.
const shortCommit = 7

// The length of the abbreviated commit hash a Go pseudo-version carries; see
// https://go.dev/ref/mod#pseudo-versions.
const pseudoHashLen = 12

var (
	// Version is the release tag, e.g. "0.1.0", or "dev" for a build that has
	// none. A checkout has no tag to claim, so a plain `go build` leaves the
	// default and reports dev with the commit it was built from.
	Version = devVersion

	// Commit is the short git SHA the binary was built from.
	Commit = noneCommit

	// Date is the UTC timestamp of the build (or, when it was inferred rather
	// than stamped, of the commit), RFC 3339.
	Date = unknownDate
)

// init resolves the stamps against the binary's own build metadata, once, so
// String, UserAgent and the `version` command all read the same values.
func init() {
	Version, Commit, Date = resolve(Version, Commit, Date, buildInfo())
}

// buildInfo returns the module and VCS metadata the toolchain recorded in this
// binary, or nil when it recorded none (module information disabled, or a
// GOPATH-mode build).
func buildInfo() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info
}

// resolve merges the -ldflags stamps with the binary's build info and returns
// the three values the CLI reports.
//
// A stamp always wins, so a release build reports exactly what GoReleaser or
// the Makefile wrote. Build info fills in what was left unstamped:
//
//   - Version is the main module's version: the tag for
//     `go install ...@v0.1.0`, or a pseudo-version such as
//     v0.0.0-20260929170114-ea07c796c36a for a commit installed by hash.
//     "(devel)", which is what a build in a checkout reports, says nothing.
//   - Commit is the vcs.revision setting, or — for a module fetched from a
//     proxy, which carries no vcs settings — the commit hash inside a
//     pseudo-version.
//   - Date is the vcs.time setting.
//
// The version is returned without the leading "v" the tags and module versions
// carry, matching the form GoReleaser stamps.
func resolve(stampVersion, stampCommit, stampDate string, bi *debug.BuildInfo) (version, commit, date string) {
	module := moduleVersion(bi)

	version = stamped(stampVersion, devVersion)
	if version == "" {
		version = module
	}
	if version == "" {
		version = devVersion
	}
	version = strings.TrimPrefix(version, "v")

	commit = stamped(stampCommit, noneCommit)
	if commit == "" {
		commit = shortRevision(vcsSetting(bi, "vcs.revision"))
	}
	if commit == "" {
		commit = pseudoCommit(module)
	}
	if commit == "" {
		commit = noneCommit
	}

	date = stamped(stampDate, unknownDate)
	if date == "" {
		date = vcsSetting(bi, "vcs.time")
	}
	if date == "" {
		date = unknownDate
	}
	return version, commit, date
}

// stamped returns the value a -ldflags stamp wrote into a variable, or "" when
// there was no stamp: an empty value, or the packaged default the variable is
// declared with.
func stamped(value, sentinel string) string {
	if value == "" || value == sentinel {
		return ""
	}
	return value
}

// moduleVersion is the version of the module the main package belongs to, or ""
// when it names nothing useful.
func moduleVersion(bi *debug.BuildInfo) string {
	if bi == nil || bi.Main.Version == "" || bi.Main.Version == "(devel)" {
		return ""
	}
	return bi.Main.Version
}

// vcsSetting returns one of the vcs.* settings the toolchain embeds in a build
// that has version-control information, or "" when it does not.
func vcsSetting(bi *debug.BuildInfo, key string) string {
	if bi == nil {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// shortRevision truncates a revision to the length the CLI reports. A revision
// from a VCS whose identifiers are not hashes is left as it is.
func shortRevision(revision string) string {
	if len(revision) <= shortCommit {
		return revision
	}
	return revision[:shortCommit]
}

// pseudoCommit pulls the commit hash out of a pseudo-version such as
// v0.0.0-20260929170114-ea07c796c36a. A module served by a proxy carries no vcs
// settings, so the hash in the version is the only commit there is. A tag, or
// any version that ends in something other than a pseudo-version's twelve hex
// digits, has no commit to offer.
func pseudoCommit(version string) string {
	if i := strings.IndexByte(version, '+'); i >= 0 {
		// A +incompatible or +metadata suffix is not part of the hash.
		version = version[:i]
	}
	i := strings.LastIndexByte(version, '-')
	if i < 0 {
		return ""
	}
	hash := version[i+1:]
	if len(hash) != pseudoHashLen || strings.Trim(hash, "0123456789abcdef") != "" {
		return ""
	}
	return hash[:shortCommit]
}

// String renders the version for `--version`. The commit is appended when it is
// known, because "which build is this" is the first question a bug report needs
// answered and a bare tag cannot answer it for a dirty or unpushed build.
func String() string {
	v := strings.TrimPrefix(Version, "v")
	if v == "" {
		v = devVersion
	}
	if Commit == "" || Commit == noneCommit {
		return v
	}
	return v + " (" + Commit + ")"
}

// UserAgent is the value of the User-Agent header on every API call. The
// backend logs it and reads it when a human views a report, so it carries
// enough to tell a stale CLI from a current one: version, OS and architecture.
func UserAgent() string {
	v := strings.TrimPrefix(Version, "v")
	if v == "" {
		v = devVersion
	}
	return "prepublish-cli/" + v + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}
