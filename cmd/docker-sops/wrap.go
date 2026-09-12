package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mohsen0/docker-sops/internal/argscan"
	"github.com/mohsen0/docker-sops/internal/reexec"
	"github.com/mohsen0/docker-sops/internal/tempstore"
)

// pluginArgv is the argv the docker CLI invoked this plugin with. Global
// docker flags that precede the plugin name are replayed to wrapped commands.
var pluginArgv = os.Args

// wrapOptions are the plugin's own flags, accepted before the wrapped docker
// command: docker sops [OPTIONS] COMMAND [ARGS...].
type wrapOptions struct {
	quiet    bool
	tmpdir   string
	noDetect bool
	patterns []string
	dryRun   bool
}

func (o *wrapOptions) flagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("docker sops", pflag.ContinueOnError)
	fs.SetInterspersed(false)
	fs.Usage = func() {} // cobra prints the help; pflag must stay silent on --help
	fs.BoolVarP(&o.quiet, "quiet", "q", envBool("DOCKER_SOPS_QUIET"), "Suppress the decrypted-files notice")
	fs.StringVar(&o.tmpdir, "tmpdir", os.Getenv("DOCKER_SOPS_TMPDIR"), "Directory for decrypted copies (default: OS temp dir)")
	fs.BoolVar(&o.noDetect, "no-detect", envBool("DOCKER_SOPS_NO_DETECT"), "Disable content detection; rely on --pattern only")
	fs.StringArrayVar(&o.patterns, "pattern", envList("DOCKER_SOPS_PATTERN"), "Glob on the file name that marks a file as encrypted (repeatable)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "Print the rewritten docker command instead of running it")
	return fs
}

func envBool(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v == "1" || v == "true" || v == "yes"
}

func envList(name string) []string {
	v := os.Getenv(name)
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// wrapUsage documents the wrapper mode in the root command's long help.
const wrapUsage = `
Wrapper mode: prefix any docker command with "sops". Every argument that is a
path to a sops-encrypted file is replaced with a decrypted copy for the
duration of the command; plain files pass through untouched.

  docker sops run --env-file secrets.enc.env myimage
  docker sops compose -f compose.yaml --env-file .env.enc up -d
  docker sops build --secret id=npmrc,src=npmrc.enc .

Wrapper options must come before the docker command; docker's own flags
are passed through untouched.`

func runWrapper(cmd *cobra.Command, args []string) error {
	var opts wrapOptions
	fs := opts.flagSet()
	fs.SetOutput(cmd.ErrOrStderr())
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return cmd.Help()
		}
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return cmd.Help()
	}
	if rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return cmd.Help()
	}

	store, err := tempstore.New(opts.tmpdir)
	if err != nil {
		return err
	}
	defer store.Close()

	rw, err := argscan.Plan(rest, store, argscan.Options{Detect: !opts.noDetect, Patterns: opts.patterns})
	if err != nil {
		return withKeyHint(err)
	}
	argv := rw.Args
	decrypted := rw.Decrypted

	if argv[0] == "compose" {
		argv, decrypted, err = rewriteCompose(cmd.Context(), argv, decrypted, store, opts)
		if err != nil {
			return withKeyHint(err)
		}
	}

	full := append(reexec.GlobalFlags(pluginArgv, pluginName), argv...)

	if opts.dryRun {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "docker "+strings.Join(redact(full, decrypted), " "))
		return err
	}
	if n := len(decrypted); n > 0 && !opts.quiet {
		fmt.Fprintf(cmd.ErrOrStderr(), "docker sops: decrypted %d file(s)\n", n)
	}

	bin, err := reexec.DockerBinary()
	if err != nil {
		return err
	}
	code, err := reexec.Run(cmd.Context(), bin, full, reexec.Options{
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
		Env:    childEnv,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return exitCodeError(code)
	}
	return nil
}

// redact replaces temp paths with <decrypted:basename> for stable dry-run output.
func redact(argv []string, decrypted []argscan.Decrypted) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		for _, d := range decrypted {
			if d.Path != "" && strings.Contains(a, d.Path) {
				a = strings.ReplaceAll(a, d.Path, "<decrypted:"+baseName(d.Path)+">")
			}
		}
		out[i] = a
	}
	return out
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
