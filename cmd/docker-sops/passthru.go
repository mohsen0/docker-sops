package main

import (
	"github.com/spf13/cobra"

	"github.com/mohsen0/docker-sops/internal/passthru"
)

// newSopsPassthruCommand builds a command that forwards every argument to
// `sops <name> ...` using the locally installed sops binary.
func newSopsPassthruCommand(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:                name + " [SOPS OPTIONS] FILE",
		Short:              short + " (requires the sops binary)",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := passthru.Run(cmd.Context(), append([]string{name}, args...), passthru.Options{
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
		},
	}
}
