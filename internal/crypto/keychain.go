package crypto

import (
	"errors"
	"fmt"
	"os"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

const (
	keychainService = "enclaude"
	keychainAccount = "age-private-key"
	envKeyVar       = "ENCLAUDE_KEY"
)

// Source identifiers returned by Load/Store operations.
const (
	SourceEnv     = "env"
	SourceKeyring = "keyring"
	SourceFile    = "file"
)

// PassphraseFunc prompts the user for a passphrase. When confirm is true the
// implementation should prompt twice and verify the two inputs match.
type PassphraseFunc func(prompt string, confirm bool) (string, error)

// DefaultPassphraseFunc is consulted by StoreKey/LoadKey when the OS keyring
// is unavailable and a passphrase is needed for the file fallback. The cmd
// layer wires this to ui.ReadPassphrase at startup.
var DefaultPassphraseFunc PassphraseFunc

// StoreKey saves the age private key. It prefers the OS keyring; if the
// keyring is unavailable (e.g. headless Linux without Secret Service), it
// falls back to a passphrase-encrypted file under $XDG_CONFIG_HOME/enclaude.
// Returns SourceKeyring or SourceFile indicating where the key was written.
func StoreKey(identity *age.X25519Identity) (string, error) {
	if err := keyring.Set(keychainService, keychainAccount, identity.String()); err == nil {
		return SourceKeyring, nil
	} else if DefaultPassphraseFunc == nil {
		return "", fmt.Errorf("OS keyring unavailable and no passphrase prompter configured: %w", err)
	}

	pp, err := DefaultPassphraseFunc(
		"OS keyring unavailable. Enter passphrase to encrypt key file: ", true)
	if err != nil {
		return "", fmt.Errorf("passphrase prompt: %w", err)
	}
	if err := StoreKeyFile(identity, pp); err != nil {
		return "", err
	}
	return SourceFile, nil
}

// LoadKey retrieves the age private key, trying (in order):
//  1. ENCLAUDE_KEY environment variable
//  2. OS keyring
//  3. passphrase-encrypted file fallback
func LoadKey() (*age.X25519Identity, string, error) {
	if envKey := os.Getenv(envKeyVar); envKey != "" {
		id, err := ParseIdentity(envKey)
		if err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", envKeyVar, err)
		}
		return id, SourceEnv, nil
	}

	if secret, err := keyring.Get(keychainService, keychainAccount); err == nil {
		id, err := ParseIdentity(secret)
		if err != nil {
			return nil, "", fmt.Errorf("parsing keyring key: %w", err)
		}
		return id, SourceKeyring, nil
	}

	if KeyFileExists() {
		if DefaultPassphraseFunc == nil {
			return nil, "", errors.New("encrypted key file present but no passphrase prompter configured")
		}
		pp, err := DefaultPassphraseFunc("Enter passphrase to unlock key file: ", false)
		if err != nil {
			return nil, "", fmt.Errorf("passphrase prompt: %w", err)
		}
		id, err := LoadKeyFile(pp)
		if err != nil {
			return nil, "", err
		}
		return id, SourceFile, nil
	}

	return nil, "", fmt.Errorf("no key found (checked %s env var, OS keyring, and %s)",
		envKeyVar, mustKeyFilePath())
}

// LoadPublicKey loads just the public key (for encryption-only operations).
func LoadPublicKey() (*age.X25519Recipient, string, error) {
	id, source, err := LoadKey()
	if err != nil {
		return nil, "", err
	}
	return id.Recipient(), source, nil
}

// DeleteKey removes the age private key from both the OS keyring and the
// file fallback. Missing entries in either backend are ignored.
func DeleteKey() error {
	kErr := keyring.Delete(keychainService, keychainAccount)
	if errors.Is(kErr, keyring.ErrNotFound) {
		kErr = nil
	}
	fErr := DeleteKeyFile()

	switch {
	case kErr == nil && fErr == nil:
		return nil
	case kErr == nil:
		return fErr
	case fErr == nil:
		return kErr
	default:
		return fmt.Errorf("keyring: %v; file: %v", kErr, fErr)
	}
}

func mustKeyFilePath() string {
	p, err := KeyFilePath()
	if err != nil {
		return "key file"
	}
	return p
}
