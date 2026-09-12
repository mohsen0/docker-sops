// Command docker-sops is a Docker CLI plugin that lets docker commands consume
// sops-encrypted files transparently. Invoked as `docker sops ...`.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"

	"github.com/mohsen0/docker-sops/internal/version"
)

const (
	pluginName = "sops"
	vendor     = "mohsen0"
	homepage   = "https://github.com/mohsen0/docker-sops"
	shortDesc  = "Use sops-encrypted files with docker commands"
)

// childEnv is the environment handed to wrapped processes. It is captured
// before any secret material (for example a keychain-stored age key) is
// injected into this process's environment.
var childEnv = os.Environ()

func main() {
	meta := metadata.Metadata{
		SchemaVersion:    "0.1.0",
		Vendor:           vendor,
		Version:          version.Version,
		ShortDescription: shortDesc,
		URL:              homepage,
	}
	// Same as plugin.Run, except that a wrapped command's exit status is
	// propagated silently instead of being printed as an error.
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := newRootCommand(dockerCLI)
	root.SetContext(context.Background())
	if err := plugin.RunPlugin(dockerCLI, root, meta); err != nil {
		var code exitCodeError
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		_, _ = fmt.Fprintln(dockerCLI.Err(), err)
		os.Exit(1)
	}
}

func newRootCommand(dockerCLI command.Cli) *cobra.Command {
	root := buildRootCommand(dockerCLI, plugin.PersistentPreRunE)
	return root
}

// newRootCommandForTest builds the root command without the docker plugin
// framework hooks, so tests can execute it directly.
func newRootCommandForTest() *cobra.Command {
	return buildRootCommand(nil, nil)
}

func buildRootCommand(dockerCLI command.Cli, pluginPreRun func(*cobra.Command, []string) error) *cobra.Command {
	root := &cobra.Command{
		Use:   pluginName + " [OPTIONS] COMMAND [ARGS...]",
		Short: shortDesc,
		Long: `docker sops decrypts sops-encrypted files on the fly so that docker
commands can consume them without leaving plaintext behind.
` + wrapUsage,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if pluginPreRun != nil {
				if err := pluginPreRun(cmd, args); err != nil {
					return err
				}
			}
			injectKeychainKey()
			return nil
		},
		RunE: runWrapper,
	}
	// Declared so cobra's subcommand lookup knows which wrapper flags take a
	// value; actual parsing happens in runWrapper.
	root.Flags().AddFlagSet((&wrapOptions{}).flagSet())
	root.AddCommand(
		newVersionCommand(),
		newDecryptCommand(),
		newSopsPassthruCommand("encrypt", "Encrypt a file"),
		newSopsPassthruCommand("edit", "Edit an encrypted file"),
		newKeyCommand(),
	)
	return root
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the plugin version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "docker-sops version %s\n", version.Version)
			return err
		},
	}
}
