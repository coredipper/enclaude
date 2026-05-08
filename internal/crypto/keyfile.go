package crypto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
)

const keyFileName = "key.age.enc"

// KeyFilePath returns the path to the passphrase-encrypted key file used as a
// fallback when the OS keyring is unavailable. Honors ENCLAUDE_KEY_FILE for an
// explicit override, otherwise $XDG_CONFIG_HOME/enclaude/key.age.enc, falling
// back to ~/.config/enclaude/key.age.enc.
func KeyFilePath() (string, error) {
	if override := os.Getenv("ENCLAUDE_KEY_FILE"); override != "" {
		return override, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "enclaude", keyFileName), nil
}

// KeyFileExists reports whether the fallback key file is present.
func KeyFileExists() bool {
	p, err := KeyFilePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// StoreKeyFile encrypts identity with passphrase (age scrypt) and writes it
// to the fallback location with 0600 permissions.
func StoreKeyFile(identity *age.X25519Identity, passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase required for file-based key storage")
	}
	encrypted, err := EncryptWithPassphrase([]byte(identity.String()), passphrase)
	if err != nil {
		return fmt.Errorf("encrypting key: %w", err)
	}
	p, err := KeyFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("creating key file directory: %w", err)
	}
	if err := os.WriteFile(p, encrypted, 0600); err != nil {
		return fmt.Errorf("writing key file: %w", err)
	}
	return nil
}

// LoadKeyFile reads and decrypts the fallback key file.
func LoadKeyFile(passphrase string) (*age.X25519Identity, error) {
	p, err := KeyFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("reading key file: %w", err)
	}
	plain, err := DecryptWithPassphrase(data, passphrase)
	if err != nil {
		return nil, fmt.Errorf("decrypting key file (wrong passphrase?): %w", err)
	}
	return ParseIdentity(string(plain))
}

// DeleteKeyFile removes the fallback key file. Missing file is not an error.
func DeleteKeyFile() error {
	p, err := KeyFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
