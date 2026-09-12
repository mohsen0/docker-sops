package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mohsen0/docker-sops/internal/keychain"
)

func newKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the age key stored in the OS keychain",
		Long: `Store an age private key in the operating system's credential store
(macOS Keychain, Windows Credential Manager, or the freedesktop Secret
Service on Linux). When a key is stored and none of SOPS_AGE_KEY,
SOPS_AGE_KEY_FILE or SOPS_AGE_KEY_CMD is set, docker sops uses it to
decrypt files.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newKeySetCommand(), newKeyShowCommand(), newKeyRmCommand())
	return cmd
}

func newKeySetCommand() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Store an age private key (read from stdin or --file)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				data []byte
				err  error
			)
			if file != "" {
				data, err = os.ReadFile(file)
			} else {
				data, err = io.ReadAll(cmd.InOrStdin())
			}
			if err != nil {
				return err
			}
			if err := keychain.Set(string(data)); err != nil {
				return err
			}
			pub, err := keychain.PublicKeys(strings.TrimSpace(string(data)))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "stored age key in the keychain; public key: %s\n", strings.Join(pub, ", "))
			return err
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read the key from this file (for example age-keygen output) instead of stdin")
	return cmd
}

func newKeyShowCommand() *cobra.Command {
	var private bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the public key of the stored age key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids, err := keychain.Get()
			if err != nil {
				return err
			}
			if private {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), ids)
				return err
			}
			pub, err := keychain.PublicKeys(ids)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(pub, "\n"))
			return err
		},
	}
	cmd.Flags().BoolVar(&private, "private", false, "Print the private key instead (for backups)")
	return cmd
}

func newKeyRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rm",
		Aliases: []string{"delete", "remove"},
		Short:   "Remove the stored age key",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return keychain.Delete()
		},
	}
}

// injectKeychainKey exports the keychain-stored age identity as SOPS_AGE_KEY
// for this process when no sops age key source is configured. It reports
// whether it did so. Callers must snapshot the environment for child
// processes before calling it so the key is never inherited.
func injectKeychainKey() bool {
	for _, v := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY_CMD"} {
		if _, set := os.LookupEnv(v); set {
			return false
		}
	}
	ids, err := keychain.Get()
	if err != nil {
		if !errors.Is(err, keychain.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "docker sops: keychain lookup failed: %v\n", err)
		}
		return false
	}
	return os.Setenv("SOPS_AGE_KEY", ids) == nil
}
