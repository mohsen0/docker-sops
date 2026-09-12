package main

import (
	"errors"
	"fmt"
)

// exitCodeError carries the exit status of a wrapped child process. main
// exits with that status without printing anything, because the child has
// already reported its own error.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func asExitCode(err error, out *exitCodeError) bool { return errors.As(err, out) }
