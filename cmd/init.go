package cmd

import (
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
	source := getSource()
	sourceDir := getSourceDir()
	sealDir := getSealDir()

	// Check source directory exists
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return fmt.Errorf("%s directory not found at %s", source, sourceDir)
	}

	// Check seal directory doesn't already exist
	if _, err := os.Stat(filepath.Join(sealDir, "seal.toml")); err == nil {
		return fmt.Errorf("seal store already initialized at %s", sealDir)
	}
	// Refuse to re-initialize over existing data: a fresh key could not
	// decrypt the referenced objects and Seal would skip them as unchanged.
	if _, err := os.Stat(filepath.Join(sealDir, "manifest.json")); err == nil {
		return fmt.Errorf("seal store data already exists at %s (manifest.json present); import the existing key with 'enclaude key import' instead of re-initializing", sealDir)
	}

	fmt.Println("Initializing enclaude...")
	fmt.Printf("  Source:     %s\n", source)
	fmt.Printf("  Source dir: %s\n", sourceDir)
	fmt.Printf("  Seal dir:   %s\n", sealDir)

	// 1. Generate age keypair
	// 1. Pick the key. ENCLAUDE_KEY overrides everything, so when set init must
	// use it (a generated key would be shadowed by the env var on every later
	// command). Otherwise generate a fresh keypair and store it.
	var identity *age.X25519Identity
	if id, set, err := crypto.EnvIdentity(); err != nil {
		return err
	} else if set {
		identity = id
		fmt.Println("\nUsing key from ENCLAUDE_KEY.")
		fmt.Printf("  Public key: %s\n", identity.Recipient().String())
	} else {
		fmt.Println("\nGenerating age keypair...")
		id, err := crypto.GenerateKey()
		if err != nil {
			return fmt.Errorf("generating key: %w", err)
		}
		identity = id
		fmt.Printf("  Public key: %s\n", identity.Recipient().String())

		// 2. Store private key (OS keyring, or passphrase-encrypted file fallback)
		fmt.Println("Storing private key...")
		keySource, err := crypto.StoreKey(identity)
		if err != nil {
			return fmt.Errorf("storing key: %w", err)
		}
		switch keySource {
		case crypto.SourceKeyring:
			fmt.Println("  Stored in OS keyring.")
		case crypto.SourceFile:
			path, _ := crypto.KeyFilePath()
			fmt.Printf("  Stored in encrypted key file: %s\n", path)
		}
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
	cfg := config.DefaultConfigForSource(source, sourceDir, sealDir)
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
	if err := os.WriteFile(filepath.Join(sealDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	// Write README
	readme := buildReadmeForSource(identity.Recipient().String(), cfg.Seal.DeviceID, source)
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

// gitignoreContent excludes the unencrypted key and force-tracks the seal store
// metadata. The negations outrank a global core.excludesFile, so a user-wide rule
// (e.g. *.json) can't silently drop manifest.json from the pushed repo.
const gitignoreContent = `# Never commit the unencrypted key
*.key

# Device-local project-key map — never synced
projectmap.local.toml

# Always track seal store metadata, even under a global gitignore
!manifest.json
!seal.toml
!.gitattributes
!README.md

# Local-only state (present only if .git is unavailable)
.enclaude-*.json
`

const readmeTemplate = `# enclaude seal store

This repository contains an encrypted backup of a {SOURCE_NAME} configuration
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
| Source     | {SOURCE}                 |
| Public key | {PUBLIC_KEY} |
| Device ID  | {DEVICE_ID} |

## Restoring on a new machine

1. Install enclaude.
2. Clone this repository to {TICK}{SEAL_DIR}{TICK}:
   {TICK}git clone <remote-url> {SEAL_DIR}{TICK}
3. Import your private key (into the OS keyring if available, otherwise into
   a local passphrase-encrypted key file):
   {TICK}enclaude key import --from-backup{TICK} (or {TICK}enclaude key import keyfile.txt{TICK})
4. Decrypt and restore your {SOURCE_NAME} files:
   {TICK}enclaude unseal{TICK}

{PI_NOTE}

## Key recovery

If both the OS keyring entry and the local key file are lost, restore from the
passphrase-encrypted backup committed to this repository:

{FENCE}
enclaude key import --from-backup
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

func buildReadmeForSource(publicKey, deviceID, source string) string {
	source = config.NormalizeSource(source)
	sourceName := "Claude Code"
	sealDir := "~/.enclaude"
	piNote := ""
	if source == config.SourcePi {
		sourceName = "pi.dev"
		sealDir = "~/.enclaude-pi"
		piNote = "For pi.dev, " + "`~/.enclaude-pi`" + " is the default seal store. Use\n" +
			"`--source pi` (or `ENCLAUDE_SOURCE=pi`) with every command."
	}
	return strings.NewReplacer(
		"{PUBLIC_KEY}", publicKey,
		"{DEVICE_ID}", deviceID,
		"{SOURCE}", source,
		"{SOURCE_NAME}", sourceName,
		"{SEAL_DIR}", sealDir,
		"{PI_NOTE}", piNote,
		"{TICK}", "`",
		"{FENCE}", "```",
	).Replace(readmeTemplate)
}
