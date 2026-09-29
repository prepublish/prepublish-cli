package cli

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"charm.land/huh/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// TestExitCode pins the mapping scripts depend on: the code has to say what the
// caller should do next, not merely that something failed.
func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"no error", nil, ExitOK},
		{"unauthorized", &api.APIError{Status: 401, Code: api.CodeUnauthorized}, ExitAuth},
		{"forbidden", &api.APIError{Status: 403, Code: api.CodeForbidden}, ExitAuth},
		{"access denied", &api.APIError{Status: 403, Code: api.CodeAccessDenied}, ExitAuth},
		{"quota spent", &api.APIError{Status: 429, Code: api.CodeFreeAnalysisUsed}, ExitLimit},
		{"payment required", &api.APIError{Status: 402, Code: api.CodeSubscriptionRequired}, ExitLimit},
		{"no subscription", &api.APIError{Status: 403, Code: api.CodeNoSubscription}, ExitLimit},
		{"email required", &api.APIError{Status: 402, Code: api.CodeEmailRequired}, ExitLimit},
		{"not found", &api.APIError{Status: 404, Code: api.CodeNotFound}, ExitFailure},
		{"rate limited", &api.APIError{Status: 429, Code: api.CodeRateLimited}, ExitFailure},
		{"network", &api.APIError{Code: api.CodeNetworkError}, ExitFailure},
		{"plain error", errors.New("boom"), ExitFailure},
		{"usage", usageErr(errors.New("unknown flag: --nope")), ExitUsage},
		// A command that already decided wins over the code inside it: `report`
		// refusing a bad id is a usage error even though it wraps nothing.
		{"wrapped decision", exitErr(ExitAuth, &api.APIError{Status: 429, Code: api.CodeFreeAnalysisUsed}), ExitAuth},
		{"cancelled", context.Canceled, ExitInterrupted},
		{"aborted prompt", huh.ErrUserAborted, ExitInterrupted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitErrorUnwraps keeps errors.Is working through the exit code, which is
// what lets a caller ask "was this a usage error or a missing file".
func TestExitErrorUnwraps(t *testing.T) {
	err := usageErr(fs.ErrNotExist)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("errors.Is(%v, fs.ErrNotExist) = false", err)
	}
	if err.Error() != fs.ErrNotExist.Error() {
		t.Fatalf("Error() = %q, want the wrapped message", err.Error())
	}
}

// TestUsageErrNil documents that marking nothing as a usage error is nothing,
// so a validator can pass its result straight through.
func TestUsageErrNil(t *testing.T) {
	if err := usageErr(nil); err != nil {
		t.Fatalf("usageErr(nil) = %v, want nil", err)
	}
}
