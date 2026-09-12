package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mohsen0/docker-sops/internal/sopsfile"
)

func newDecryptCommand() *cobra.Command {
	var (
		inPlace bool
		output  string
	)
	cmd := &cobra.Command{
		Use:   "decrypt [OPTIONS] FILE",
		Short: "Decrypt a sops-encrypted file",
		Long: `Decrypt a sops-encrypted file and print the plaintext to stdout.
The format (yaml, json, dotenv, ini, binary) is inferred from the file name
and content. Keys are resolved exactly as the sops binary would.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inPlace && output != "" {
				return errors.New("--in-place and --output are mutually exclusive")
			}
			plain, err := sopsfile.Decrypt(args[0])
			if err != nil {
				return withKeyHint(err)
			}
			switch {
			case inPlace:
				return os.WriteFile(args[0], plain, 0o600)
			case output != "":
				return os.WriteFile(output, plain, 0o600)
			default:
				_, err := cmd.OutOrStdout().Write(plain)
				return err
			}
		},
	}
	cmd.Flags().BoolVarP(&inPlace, "in-place", "i", false, "Overwrite FILE with its plaintext")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write plaintext to this file (mode 0600) instead of stdout")
	return cmd
}

// withKeyHint appends key-lookup guidance to decryption failures that are
// not about the file itself.
func withKeyHint(err error) error {
	if errors.Is(err, sopsfile.ErrNotEncrypted) {
		return err
	}
	return fmt.Errorf("%w\nhint: keys are resolved like the sops binary does (SOPS_AGE_KEY_FILE, SOPS_AGE_KEY_CMD, KMS credentials, .sops.yaml); see docs/keychain.md", err)
}
