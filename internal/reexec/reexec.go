// Package reexec resolves the docker CLI to re-exec, extracts the global
// flags that preceded the plugin name in the original invocation, and runs a
// child process with stdio wired through, signals forwarded, and the child's
// exit code reflected back to the caller.
package reexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// dockerCLIOriginalCommandEnv is set by the docker CLI when it execs a
// plugin, and holds the path to the docker binary that invoked it.
const dockerCLIOriginalCommandEnv = "DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND"

// DockerBinary returns the docker CLI to re-exec: the value of
// DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND (set by the docker CLI when it runs
// a plugin) if non-empty, otherwise "docker" resolved on PATH via
// exec.LookPath. It returns an error if neither is available.
func DockerBinary() (string, error) {
	if v := os.Getenv(dockerCLIOriginalCommandEnv); v != "" {
		return v, nil
	}
	path, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("resolve docker binary: %w", err)
	}
	return path, nil
}

// GlobalFlags extracts the docker global flags that preceded the plugin name
// in the original argv. args is os.Args as the plugin received them, e.g.
// ["docker-sops", "--context", "prod", "-D", "sops", "run", "x"]; with
// pluginName "sops" it returns ["--context", "prod", "-D"]. If pluginName is
// not found (as a standalone token, after args[0]), it returns nil.
//
// Everything before the first occurrence of pluginName is treated as a
// global flag; both "--flag=value" and "--flag value" forms are passed
// through untouched, since GlobalFlags does not need to know which flags
// take values.
func GlobalFlags(args []string, pluginName string) []string {
	for i := 1; i < len(args); i++ {
		if args[i] == pluginName {
			if i == 1 {
				return nil
			}
			flags := make([]string, i-1)
			copy(flags, args[1:i])
			return flags
		}
	}
	return nil
}

// Options configures Run. Zero-valued fields fall back to the process
// defaults described per field.
type Options struct {
	Stdin  io.Reader // default os.Stdin
	Stdout io.Writer // default os.Stdout
	Stderr io.Writer // default os.Stderr
	Env    []string  // default os.Environ()
	Dir    string    // working directory, default inherit
}

// Run executes binary with args, wiring stdio, forwarding SIGINT and SIGTERM
// received by this process to the child, and waiting for it. It returns the
// child's exit code (0 on success). If the child was killed by a signal, it
// returns 128 + signal number. A non-zero exit is NOT an error; error is
// returned only when the child could not be started. ctx cancellation kills
// the child.
func Run(ctx context.Context, binary string, args []string, opts Options) (int, error) {
	cmd := exec.CommandContext(ctx, binary, args...)

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	} else {
		cmd.Stdin = os.Stdin
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	} else {
		cmd.Stdout = os.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	} else {
		cmd.Env = os.Environ()
	}
	cmd.Dir = opts.Dir

	// Install signal forwarding before starting the child so there is no
	// window in which a SIGINT/SIGTERM sent to this process is lost.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("start %s: %w", binary, err)
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				// Best effort: the child may have already exited.
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	return exitCode(cmd, waitErr)
}

// exitCode derives the process exit code Run should report from the
// completed command and the error returned by cmd.Wait.
func exitCode(cmd *exec.Cmd, waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		// cmd.Wait failed for a reason other than a non-zero exit or
		// signal (e.g. an I/O error copying stdio); the child did start,
		// so this is not a "could not be started" error. Best effort:
		// surface it as a distinguishable failure exit code.
		return -1, nil
	}

	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return status.ExitStatus(), nil
	}
	return exitErr.ExitCode(), nil
}
