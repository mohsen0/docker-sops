// Command docker-sops is a Docker CLI plugin that lets docker commands consume
// sops-encrypted files transparently. Invoked as `docker sops ...`.
package main

import (
	"fmt"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"

	"github.com/mohsen0/sops-docker-cli-plugin/internal/version"
)

const (
	pluginName = "sops"
	vendor     = "mohsen0"
	homepage   = "https://github.com/mohsen0/sops-docker-cli-plugin"
	shortDesc  = "Use sops-encrypted files with docker commands"
)

func main() {
	plugin.Run(newRootCommand, metadata.Metadata{
		SchemaVersion:    "0.1.0",
		Vendor:           vendor,
		Version:          version.Version,
		ShortDescription: shortDesc,
		URL:              homepage,
	})
}

func newRootCommand(dockerCLI command.Cli) *cobra.Command {
	root := &cobra.Command{
		Use:   pluginName,
		Short: shortDesc,
		Long: `docker sops decrypts sops-encrypted files on the fly so that docker
commands can consume them without leaving plaintext behind.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return plugin.PersistentPreRunE(cmd, args)
		},
	}
	root.AddCommand(newVersionCommand(dockerCLI))
	return root
}

func newVersionCommand(dockerCLI command.Cli) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the plugin version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(dockerCLI.Out(), "docker-sops version %s\n", version.Version)
			return err
		},
	}
}
