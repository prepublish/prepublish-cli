// Package version carries the build metadata a release stamps into the binary.
//
// The three variables are set with -ldflags -X (see the Makefile). A plain
// `go build` or `go run` leaves the defaults below, which is what a developer
// build should report: there is no meaningful tag or commit to claim.
package version

import "runtime"

// Version is the release tag, or "dev" for a build that has none.
var Version = "dev"

// Commit is the short git SHA the binary was built from.
var Commit = "none"

// Date is the UTC build timestamp, RFC 3339.
var Date = "unknown"

// String renders the version for `--version`. The commit is appended when it is
// known, because "which build is this" is the first question a bug report
// needs answered and a bare tag cannot answer it for a dirty or unpushed build.
func String() string {
	if Commit == "" || Commit == "none" {
		return Version
	}
	return Version + " (" + Commit + ")"
}

// UserAgent is the value of the User-Agent header on every API call. The
// backend logs it and reads it when a human views a report, so it carries
// enough to tell a stale CLI from a current one: version, OS and architecture.
func UserAgent() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	return "prepublish-cli/" + v + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}
