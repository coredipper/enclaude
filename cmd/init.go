package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new vault",
	Long:  "Generate an age keypair, store it in the OS keychain, and perform the initial seal.",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	claudeDir := getClaudeDir()
	vaultDir := getVaultDir()

	// Check claude directory exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return fmt.Errorf("claude directory not found at %s", claudeDir)
	}

	// Check vault directory doesn't already exist
	if _, err := os.Stat(filepath.Join(vaultDir, "vault.toml")); err == nil {
		return fmt.Errorf("vault already initialized at %s", vaultDir)
	}

	fmt.Println("Initializing claude-vault...")
	fmt.Printf("  Claude dir: %s\n", claudeDir)
	fmt.Printf("  Vault dir:  %s\n", vaultDir)

	// 1. Generate age keypair
	fmt.Println("\nGenerating age keypair...")
	identity, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}
	fmt.Printf("  Public key: %s\n", identity.Recipient().String())

	// 2. Store in OS keychain
	fmt.Println("Storing private key in OS keychain...")
	if err := crypto.StoreKey(identity); err != nil {
		return fmt.Errorf("storing key in keychain: %w", err)
	}
	fmt.Println("  Stored in keychain.")

	// 3. Create vault directory
	if err := os.MkdirAll(vaultDir, 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	// 4. Create passphrase-encrypted backup
	fmt.Print("\nEnter backup passphrase (for key recovery): ")
	reader := bufio.NewReader(os.Stdin)
	passphrase, _ := reader.ReadString('\n')
	passphrase = strings.TrimSpace(passphrase)

	if passphrase != "" {
		backup, err := crypto.EncryptWithPassphrase([]byte(identity.String()), passphrase)
		if err != nil {
			return fmt.Errorf("creating key backup: %w", err)
		}
		backupPath := filepath.Join(vaultDir, "key.age.backup")
		if err := os.WriteFile(backupPath, backup, 0600); err != nil {
			return fmt.Errorf("writing key backup: %w", err)
		}
		fmt.Println("  Key backup saved.")
	} else {
		fmt.Println("  Skipping backup (no passphrase entered).")
	}

	// 5. Write default config
	cfg := config.DefaultConfig(claudeDir, vaultDir)
	if err := cfg.Save(vaultDir); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Println("  Config written to vault.toml")

	// 6. Initialize git repo
	fmt.Println("\nInitializing git repository...")
	gitInit := exec.Command("git", "init", vaultDir)
	gitInit.Stdout = os.Stdout
	gitInit.Stderr = os.Stderr
	if err := gitInit.Run(); err != nil {
		return fmt.Errorf("git init: %w", err)
	}

	// Write .gitattributes for future merge driver
	gitattributes := "manifest.json merge=claude-vault-manifest\n"
	if err := os.WriteFile(filepath.Join(vaultDir, ".gitattributes"), []byte(gitattributes), 0644); err != nil {
		return fmt.Errorf("writing .gitattributes: %w", err)
	}

	// Write .gitignore
	gitignore := "# Never commit the unencrypted key\n*.key\n"
	if err := os.WriteFile(filepath.Join(vaultDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	// 7. Initial seal
	fmt.Println("\nPerforming initial seal...")
	stats, err := vault.Seal(cfg, identity.Recipient(), flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("initial seal: %w", err)
	}
	fmt.Printf("  Sealed: %s\n", stats)

	// 8. Initial commit
	gitAdd := exec.Command("git", "-C", vaultDir, "add", ".")
	if err := gitAdd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	gitCommit := exec.Command("git", "-C", vaultDir, "commit", "-m",
		fmt.Sprintf("vault: initial seal from %s", cfg.Vault.DeviceID))
	gitCommit.Stdout = os.Stdout
	gitCommit.Stderr = os.Stderr
	if err := gitCommit.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	fmt.Println("\nVault initialized successfully!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. claude-vault remote add origin <url>   # set up sync remote")
	fmt.Println("  2. claude-vault hooks install              # enable auto-sync")
	return nil
}
