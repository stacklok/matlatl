// Command doctopus is the CLI entrypoint. It builds the Cobra command tree and
// maps the outcome to the process exit code per ADR 0005.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/stacklok/doctopus/internal/platform"
)

func main() {
	os.Exit(int(run()))
}

// run executes the root command and translates the result into an ExitCode. It
// installs a SIGINT/SIGTERM-cancellable context so an interrupt propagates to
// the pipeline and artifact writer (which honor ctx).
func run() platform.ExitCode {
	// Register SIGTERM alongside SIGINT so a container/orchestrator stop signal
	// cancels the context and lets the pipeline + writer shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runArgs(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

// runArgs builds and executes the command tree with an explicit context, args
// and I/O streams, returning the mapped ExitCode. It is the testable core of
// run(); tests pass context.Background().
func runArgs(ctx context.Context, args []string, stdout, stderr io.Writer) platform.ExitCode {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.ExecuteContext(ctx)
	if err == nil {
		return platform.ExitOK
	}

	var coded exitCodeError
	if errors.As(err, &coded) {
		// Even with cobra's SilenceErrors, the user needs an explanation for a
		// coded failure (e.g. a bad flag exiting 2). Emit it to stderr
		// (best-effort: a failing stderr does not change the exit code).
		_, _ = fmt.Fprintln(stderr, "doctopus:", coded.Error())
		return coded.code
	}

	_, _ = fmt.Fprintln(stderr, "doctopus:", err)
	return platform.ExitRuntime
}
