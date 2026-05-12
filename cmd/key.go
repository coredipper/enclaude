package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

// keyRotateDeps groups the side-effecting operations rotateKeyCore performs.
// Injected so tests can verify ordering and failure semantics without touching
// a real seal store, keyring, or filesystem.
type keyRotateDeps struct {
	loadKey  func() (*age.X25519Identity, error)
	genKey   func() (*age.X25519Identity, error)
	storeKey func(*age.X25519Identity) error
	rotate   func(old *age.X25519Identity, newRecipient *age.X25519Recipient) (int, error)
}

// rotateKeyCore performs key rotation in the only safe order:
//
//  1. Load old key.
//  2. Generate new key.
//  3. Persist the new key.
//  4. Re-encrypt all sealed objects to the new key.
//
// Persisting BEFORE re-encryption is critical: if storeKey prompts the user
// (file fallback) and they cancel, or any other persistence failure occurs,
// the seal store is still intact under the old key. Doing rotate first and
// then storeKey would leave every object encrypted to a key that exists only
// in process memory if storeKey fails — total data loss.
func rotateKeyCore(d keyRotateDeps) (old, new *age.X25519Identity, rotated int, err error) {
	old, err = d.loadKey()
	if err != nil {
		err = fmt.Errorf("loading current key: %w", err)
		return
	}
	new, err = d.genKey()
	if err != nil {
		err = fmt.Errorf("generating new key: %w", err)
		return
	}
	if err = d.storeKey(new); err != nil {
		err = fmt.Errorf("storing new key (seal store untouched): %w", err)
		return
	}
	rotated, err = d.rotate(old, new.Recipient())
	if err != nil {
		err = fmt.Errorf("rotation: %w", err)
		return
	}
	return
}

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Key management commands",
}

var keyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the seal store's public key",
	RunE: func(cmd *cobra.Command, args []string) error {
		recipient, source, err := crypto.LoadPublicKey()
		if err != nil {
			return fmt.Errorf("loading key: %w", err)
		}
		fmt.Printf("Public key: %s\n", recipient.String())
		fmt.Printf("Source: %s\n", source)
		return nil
	},
}

var keyExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the private key (for backup or new device)",
	RunE: func(cmd *cobra.Command, args []string) error {
		identity, source, err := crypto.LoadKey()
		if err != nil {
			return fmt.Errorf("loading key: %w", err)
		}
		fmt.Printf("# enclaude private key (loaded from %s)\n", source)
		fmt.Printf("# public key: %s\n", identity.Recipient().String())
		fmt.Println(identity.String())
		return nil
	},
}

var importFromBackup bool

var keyImportCmd = &cobra.Command{
	Use:   "import [file | -]",
	Short: "Import a private key from file, stdin, or backup",
	Long: `Import an age private key into the OS keychain.

  enclaude key import keyfile.txt    # from file
  echo "AGE-SECRET-KEY-..." | enclaude key import -  # from stdin
  enclaude key import --from-backup  # decrypt key.age.backup`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var keyData string

		if importFromBackup {
			backupPath := filepath.Join(getSealDir(), "key.age.backup")
			encrypted, err := os.ReadFile(backupPath)
			if err != nil {
				return fmt.Errorf("reading key backup: %w", err)
			}
			passphrase, err := ui.ReadPassphrase("Backup passphrase: ", false)
			if err != nil {
				return fmt.Errorf("reading passphrase: %w", err)
			}

			decrypted, err := crypto.DecryptWithPassphrase(encrypted, passphrase)
			if err != nil {
				return fmt.Errorf("decrypting backup (wrong passphrase?): %w", err)
			}
			keyData = string(decrypted)
		} else if len(args) == 0 {
			return fmt.Errorf("specify a file, '-' for stdin, or --from-backup")
		} else if args[0] == "-" {
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			keyData = strings.Join(lines, "\n")
		} else {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading key file: %w", err)
			}
			keyData = string(data)
		}

		identity, err := crypto.ParseIdentity(strings.TrimSpace(keyData))
		if err != nil {
			return fmt.Errorf("invalid key: %w", err)
		}

		source, err := crypto.StoreKey(identity)
		if err != nil {
			return fmt.Errorf("storing key: %w", err)
		}

		fmt.Printf("%s Key imported (stored in %s).\n", ui.Green("OK"), source)
		fmt.Printf("Public key: %s\n", identity.Recipient().String())
		return nil
	},
}

var keyRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new key and re-encrypt all sealed objects",
	RunE: func(cmd *cobra.Command, args []string) error {
		sealDir := getSealDir()

		cfg, err := config.Load(sealDir)
		if err != nil {
			return err
		}

		deps := keyRotateDeps{
			loadKey: func() (*age.X25519Identity, error) {
				id, _, err := crypto.LoadKey()
				return id, err
			},
			genKey: crypto.GenerateKey,
			storeKey: func(id *age.X25519Identity) error {
				_, err := crypto.StoreKey(id)
				return err
			},
			rotate: func(old *age.X25519Identity, newRecipient *age.X25519Recipient) (int, error) {
				fmt.Println("Re-encrypting all objects...")
				return store.Rotate(cfg, old, newRecipient, flagVerbose, nil)
			},
		}

		oldIdentity, newIdentity, rotated, err := rotateKeyCore(deps)
		if err != nil {
			return err
		}
		oldPub := oldIdentity.Recipient().String()
		newPub := newIdentity.Recipient().String()
		fmt.Printf("  Re-encrypted %d objects.\n", rotated)

		// Create new backup
		fmt.Println()
		passphrase, err := ui.ReadPassphraseOptional("New backup passphrase (or Enter to skip): ")
		if err != nil {
			return fmt.Errorf("reading backup passphrase: %w", err)
		}
		if passphrase != "" {
			backup, err := crypto.EncryptWithPassphrase([]byte(newIdentity.String()), passphrase)
			if err != nil {
				return fmt.Errorf("creating backup: %w", err)
			}
			os.WriteFile(filepath.Join(sealDir, "key.age.backup"), backup, 0600)
			fmt.Println("  Key backup updated.")
		}

		// Commit
		git := gitops.New(sealDir)
		if err := git.AddAll(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		if err := git.Commit(fmt.Sprintf("seal: key rotation from %s", cfg.Seal.DeviceID)); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}

		fmt.Printf("\n%s Key rotated.\n", ui.Green("Done."))
		fmt.Printf("  Old public key: %s\n", ui.Faint(oldPub))
		fmt.Printf("  New public key: %s\n", ui.Bold(newPub))
		return nil
	},
}

func init() {
	keyImportCmd.Flags().BoolVar(&importFromBackup, "from-backup", false, "decrypt key.age.backup")
	keyCmd.AddCommand(keyShowCmd)
	keyCmd.AddCommand(keyExportCmd)
	keyCmd.AddCommand(keyImportCmd)
	keyCmd.AddCommand(keyRotateCmd)
	rootCmd.AddCommand(keyCmd)
}
