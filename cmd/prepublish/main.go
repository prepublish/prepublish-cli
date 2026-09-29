// Command prepublish audits a YouTube script before it is recorded.
//
// main does three things: it makes the process interruptible, it hands the root
// command to fang (which owns --version, styled help and the error rendering),
// and it turns whatever came back into an exit code. Everything else lives in
// internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"

	"github.com/prepublish/prepublish-cli/internal/cli"
	"github.com/prepublish/prepublish-cli/internal/version"
)

func main() {
	// Ctrl-C cancels the context rather than killing the process, so a poll in
	// flight or a half-written file is unwound by the code that owns it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, onError := cli.Command()

	err := fang.Execute(ctx, root,
		fang.WithVersion(version.String()),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
		fang.WithErrorHandler(onError),
	)
	os.Exit(cli.ExitCode(err))
}
