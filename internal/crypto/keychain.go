package crypto

import (
	"fmt"
	"os"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

const (
	keychainService = "claude-seal"
	keychainAccount = "age-private-key"
	envKeyVar       = "CLAUDE_VAULT_KEY"
)

// StoreKey saves the age private key to the OS keychain.
func StoreKey(identity *age.X25519Identity) error {
	return keyring.Set(keychainService, keychainAccount, identity.String())
}

// LoadKey retrieves the age private key, trying (in order):
// 1. CLAUDE_VAULT_KEY environment variable
// 2. OS keychain
// Returns the identity and the source it was loaded from.
func LoadKey() (*age.X25519Identity, string, error) {
	// Try environment variable first
	if envKey := os.Getenv(envKeyVar); envKey != "" {
		id, err := ParseIdentity(envKey)
		if err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", envKeyVar, err)
		}
		return id, "env", nil
	}

	// Try OS keychain
	secret, err := keyring.Get(keychainService, keychainAccount)
	if err == nil {
		id, err := ParseIdentity(secret)
		if err != nil {
			return nil, "", fmt.Errorf("parsing keychain key: %w", err)
		}
		return id, "keychain", nil
	}

	return nil, "", fmt.Errorf("no key found (checked %s env var and OS keychain)", envKeyVar)
}

// LoadPublicKey loads just the public key (for encryption-only operations like seal).
func LoadPublicKey() (*age.X25519Recipient, string, error) {
	id, source, err := LoadKey()
	if err != nil {
		return nil, "", err
	}
	return id.Recipient(), source, nil
}

// DeleteKey removes the age private key from the OS keychain.
func DeleteKey() error {
	return keyring.Delete(keychainService, keychainAccount)
}
