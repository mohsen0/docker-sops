// Package passthru execs a locally installed sops binary, inheriting
// stdio, for commands the plugin delegates to sops directly (encrypt,
// edit).
package passthru

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
)

// SopsBinary returns the path to the sops executable to run: the value of
// $DOCKER_SOPS_BIN if set and non-empty, otherwise "sops" resolved from
// PATH.
func SopsBinary() (string, error) {
	if bin := os.Getenv("DOCKER_SOPS_BIN"); bin != "" {
		return bin, nil
	}
	path, err := exec.LookPath("sops")
	if err != nil {
		return "", errors.New("sops binary not found; install sops or set DOCKER_SOPS_BIN")
	}
	return path, nil
}

// Options configures Run's inherited stdio and environment. Any field left
// nil/unset defaults to the corresponding os stream or os.Environ().
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    []string
}

// Run execs the sops binary (resolved via SopsBinary) with args, wiring
// stdin/stdout/stderr and env from opts (falling back to the os defaults),
// and returns the child's exit code. A non-zero exit code is not itself an
// error; err is non-nil only if the sops binary could not be found or
// started.
func Run(ctx context.Context, args []string, opts Options) (int, error) {
	bin, err := SopsBinary()
	if err != nil {
		return 0, err
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	cmd.Stdin = opts.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = opts.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = opts.Env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}

	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}
