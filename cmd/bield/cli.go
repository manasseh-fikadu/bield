package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/manasseh-fikadu/bield/internal/cli"
)

// exitError means a command has already said what went wrong, and the process
// should exit with the code.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// runCLI runs one of the commands that talk to a running Bield. It prints its
// own errors, in the form the caller asked for, and exits with its own codes.
func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	env := cli.DefaultEnv(buildVersion())
	env.Stdout, env.Stderr = stdout, stderr
	if stdout != os.Stdout {
		env.Terminal = false
	}
	if code := cli.Run(ctx, env, args); code != 0 {
		return exitError(code)
	}
	return nil
}
