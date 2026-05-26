package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new seal store",
	Long:  "Generate an age keypair, store it in the OS keychain, and perform the initial seal.",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	claudeDir := getClaudeDir()
	sealDir := getSealDir()

	// Check claude directory exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return fmt.Errorf("claude directory not found at %s", claudeDir)
	}

	// Check seal directory doesn't already exist
	if _, err := os.Stat(filepath.Join(sealDir, "seal.toml")); err == nil {
		return fmt.Errorf("seal store already initialized at %s", sealDir)
	}

	fmt.Println("Initializing enclaude...")
	fmt.Printf("  Claude dir: %s\n", claudeDir)
	fmt.Printf("  Seal dir:  %s\n", sealDir)

	// 1. Generate age keypair
	fmt.Println("\nGenerating age keypair...")
	identity, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}
	fmt.Printf("  Public key: %s\n", identity.Recipient().String())

	// 2. Store private key (OS keyring, or passphrase-encrypted file fallback)
	fmt.Println("Storing private key...")
	source, err := crypto.StoreKey(identity)
	if err != nil {
		return fmt.Errorf("storing key: %w", err)
	}
	switch source {
	case crypto.SourceKeyring:
		fmt.Println("  Stored in OS keyring.")
	case crypto.SourceFile:
		path, _ := crypto.KeyFilePath()
		fmt.Printf("  Stored in encrypted key file: %s\n", path)
	}

	// 3. Create seal directory
	if err := os.MkdirAll(sealDir, 0700); err != nil {
		return fmt.Errorf("creating seal directory: %w", err)
	}

	// 4. Create passphrase-encrypted backup
	if err := createKeyBackup(sealDir, identity.String()); err != nil {
		return err
	}

	// 5. Write default config
	cfg := config.DefaultConfig(claudeDir, sealDir)
	if err := cfg.Save(sealDir); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Println("  Config written to seal.toml")

	// 6. Initialize git repo
	fmt.Println("\nInitializing git repository...")
	git := gitops.New(sealDir)
	if err := git.Init(); err != nil {
		return fmt.Errorf("git init: %w", err)
	}

	// Write .gitattributes for future merge driver
	gitattributes := "manifest.json merge=enclaude-manifest\n"
	if err := os.WriteFile(filepath.Join(sealDir, ".gitattributes"), []byte(gitattributes), 0644); err != nil {
		return fmt.Errorf("writing .gitattributes: %w", err)
	}

	// Write .gitignore
	gitignore := "# Never commit the unencrypted key\n*.key\n"
	if err := os.WriteFile(filepath.Join(sealDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	// Write README
	readme := buildReadme(identity.Recipient().String(), cfg.Seal.DeviceID)
	if err := os.WriteFile(filepath.Join(sealDir, "README.md"), []byte(readme), 0644); err != nil {
		return fmt.Errorf("writing README: %w", err)
	}

	// 7. Initial seal
	fmt.Println("\nPerforming initial seal...")
	stats, err := store.Seal(cfg, identity.Recipient(), flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("initial seal: %w", err)
	}
	fmt.Printf("  Sealed: %s\n", stats)

	// 8. Initial commit
	if err := git.AddAll(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	if err := git.Commit(fmt.Sprintf("seal: initial seal from %s", cfg.Seal.DeviceID)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	fmt.Println("\nSeal initialized successfully!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. enclaude remote add origin <url>   # set up sync remote")
	fmt.Println("  2. enclaude hooks install              # enable auto-sync")
	return nil
}

const readmeTemplate = `# enclaude seal store

This repository contains an encrypted backup of a Claude Code configuration
directory, managed by [enclaude](https://github.com/coredipper/enclaude).

All files are encrypted with [age](https://age-encryption.org/) and cannot be
read without the private key. On the originating device the key is stored in
the OS keyring (macOS Keychain, Windows Credential Manager, Linux Secret
Service) or — on hosts without one, such as headless Linux — in a
passphrase-encrypted key file under {TICK}$XDG_CONFIG_HOME/enclaude/{TICK}.

> **Private keys are never stored here.** The age private key lives in the OS
> keyring or a local passphrase-encrypted key file (plus, optionally, an
> encrypted backup file inside this repository). Do not commit private keys or
> unencrypted backups to this repository.

## Repository details

| Field      | Value                    |
|------------|--------------------------|
| Public key | {PUBLIC_KEY} |
| Device ID  | {DEVICE_ID} |

## Restoring on a new machine

1. Install enclaude.
2. Clone this repository to {TICK}~/.enclaude/{TICK}:
   {TICK}git clone <remote-url> ~/.enclaude{TICK}
3. Import your private key (into the OS keyring if available, otherwise into
   a local passphrase-encrypted key file):
   {TICK}enclaude key import{TICK}
4. Decrypt and restore your Claude files:
   {TICK}enclaude unseal{TICK}

## Key recovery

If both the OS keyring entry and the local key file are lost, restore from the
passphrase-encrypted backup committed to this repository:

{FENCE}
enclaude key recover key.age.backup
{FENCE}

You will be prompted for the passphrase you set during {TICK}enclaude init{TICK}.

## Daily use

{FENCE}
enclaude seal          # encrypt and commit changes
enclaude push          # push to remote
enclaude pull          # pull and decrypt latest
enclaude status        # show unsealed changes not yet sealed
{FENCE}
`

func createKeyBackup(sealDir, secretKey string) error {
	fmt.Println()
	passphrase, err := ui.ReadPassphraseOptional("Enter backup passphrase (for key recovery, blank to skip): ")
	if err != nil {
		return fmt.Errorf("reading backup passphrase: %w", err)
	}

	if passphrase != "" {
		backup, err := crypto.EncryptWithPassphrase([]byte(secretKey), passphrase)
		if err != nil {
			return fmt.Errorf("creating key backup: %w", err)
		}
		backupPath := filepath.Join(sealDir, "key.age.backup")
		if err := os.WriteFile(backupPath, backup, 0600); err != nil {
			return fmt.Errorf("writing key backup: %w", err)
		}
		fmt.Println("  Key backup saved.")
	} else {
		fmt.Println("  Skipping backup (no passphrase entered).")
	}

	return nil
}

func buildReadme(publicKey, deviceID string) string {
	return strings.NewReplacer(
		"{PUBLIC_KEY}", publicKey,
		"{DEVICE_ID}", deviceID,
		"{TICK}", "`",
		"{FENCE}", "```",
	).Replace(readmeTemplate)
}
