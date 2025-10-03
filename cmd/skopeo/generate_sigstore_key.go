package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"go.podman.io/image/v5/pkg/cli"
	"go.podman.io/image/v5/signature/sigstore"
)

type generateSigstoreKeyOptions struct {
	outputPrefix   string
	passphraseFile string
}

func generateSigstoreKeyCmd() *cobra.Command {
	var opts generateSigstoreKeyOptions
	cmd := &cobra.Command{
		Use:     "generate-sigstore-key [command options] --output-prefix PREFIX",
		Short:   "Generate a sigstore public/private key pair",
		RunE:    commandAction(opts.run),
		Example: "skopeo generate-sigstore-key --output-prefix my-key",
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.StringVar(&opts.outputPrefix, "output-prefix", "", "Write the keys to `PREFIX`.pub and `PREFIX`.private")
	flags.StringVar(&opts.passphraseFile, "passphrase-file", "", "Read a passphrase for the private key from `PATH`")
	return cmd
}

// ensurePathDoesNotExist verifies that path does not refer to an existing file,
// and returns an error if so.
func ensurePathDoesNotExist(path string) error {
	switch _, err := os.Stat(path); {
	case err == nil:
		return fmt.Errorf("Refusing to overwrite existing %q", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("Error checking existence of %q: %w", path, err)
	}
}

func (opts *generateSigstoreKeyOptions) run(args []string, stdout io.Writer) error {
	if len(args) != 0 || opts.outputPrefix == "" {
		return errors.New("Usage: generate-sigstore-key --output-prefix PREFIX")
	}

	pubKeyPath := opts.outputPrefix + ".pub"
	privateKeyPath := opts.outputPrefix + ".private"
	if err := ensurePathDoesNotExist(pubKeyPath); err != nil {
		return err
	}
	if err := ensurePathDoesNotExist(privateKeyPath); err != nil {
		return err
	}

	var passphrase string
	if opts.passphraseFile != "" {
		p, err := cli.ReadPassphraseFile(opts.passphraseFile)
		if err != nil {
			return err
		}
		passphrase = p
	} else {
		p, err := promptForPassphrase(privateKeyPath, os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		passphrase = p
	}

	keys, err := sigstore.GenerateKeyPair([]byte(passphrase))
	if err != nil {
		return fmt.Errorf("Error generating key pair: %w", err)
	}

	if err := os.WriteFile(privateKeyPath, keys.PrivateKey, 0600); err != nil {
		return fmt.Errorf("Error writing private key to %q: %w", privateKeyPath, err)
	}
	if err := os.WriteFile(pubKeyPath, keys.PublicKey, 0644); err != nil {
		return fmt.Errorf("Error writing private key to %q: %w", pubKeyPath, err)
	}
	fmt.Fprintf(stdout, "Key written to %q and %q\n", privateKeyPath, pubKeyPath)
	return nil
}
