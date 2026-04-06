package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
	"github.com/coredipper/claude-seal/internal/gitops"
	"github.com/coredipper/claude-seal/internal/ui"
	"github.com/coredipper/claude-seal/internal/store"
	"github.com/spf13/cobra"
)

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
		fmt.Printf("# claude-seal private key (loaded from %s)\n", source)
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

  claude-seal key import keyfile.txt    # from file
  echo "AGE-SECRET-KEY-..." | claude-seal key import -  # from stdin
  claude-seal key import --from-backup  # decrypt key.age.backup`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var keyData string

		if importFromBackup {
			backupPath := filepath.Join(getSealDir(), "key.age.backup")
			encrypted, err := os.ReadFile(backupPath)
			if err != nil {
				return fmt.Errorf("reading key backup: %w", err)
			}
			fmt.Print("Backup passphrase: ")
			reader := bufio.NewReader(os.Stdin)
			passphrase, _ := reader.ReadString('\n')
			passphrase = strings.TrimSpace(passphrase)

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

		if err := crypto.StoreKey(identity); err != nil {
			return fmt.Errorf("storing key: %w", err)
		}

		fmt.Printf("%s Key imported.\n", ui.Green("OK"))
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

		oldIdentity, _, err := crypto.LoadKey()
		if err != nil {
			return fmt.Errorf("loading current key: %w", err)
		}
		oldPub := oldIdentity.Recipient().String()

		fmt.Println("Generating new key...")
		newIdentity, err := crypto.GenerateKey()
		if err != nil {
			return fmt.Errorf("generating new key: %w", err)
		}
		newPub := newIdentity.Recipient().String()

		fmt.Println("Re-encrypting all objects...")
		rotated, err := store.Rotate(cfg, oldIdentity, newIdentity.Recipient(), flagVerbose, nil)
		if err != nil {
			return fmt.Errorf("rotation: %w", err)
		}
		fmt.Printf("  Re-encrypted %d objects.\n", rotated)

		// Store new key in keychain (replaces old)
		if err := crypto.StoreKey(newIdentity); err != nil {
			return fmt.Errorf("storing new key: %w", err)
		}

		// Create new backup
		fmt.Print("\nNew backup passphrase (or Enter to skip): ")
		reader := bufio.NewReader(os.Stdin)
		passphrase, _ := reader.ReadString('\n')
		passphrase = strings.TrimSpace(passphrase)
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
		git.AddAll()
		git.Commit(fmt.Sprintf("seal: key rotation from %s", cfg.Seal.DeviceID))

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
