package crypto

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

// GenerateKey creates a new age X25519 identity (keypair).
func GenerateKey() (*age.X25519Identity, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating age key: %w", err)
	}
	return identity, nil
}

// Encrypt encrypts plaintext using the given age public key (recipient).
func Encrypt(plaintext []byte, recipient age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("creating age writer: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing age writer: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt decrypts ciphertext using the given age identity (private key).
func Decrypt(ciphertext []byte, identity age.Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("creating age reader: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted data: %w", err)
	}
	return plaintext, nil
}

// ParseIdentity parses an age secret key string into an Identity.
func ParseIdentity(secretKey string) (*age.X25519Identity, error) {
	identities, err := age.ParseIdentities(strings.NewReader(secretKey))
	if err != nil {
		return nil, fmt.Errorf("parsing identity: %w", err)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("no identity found in input")
	}
	id, ok := identities[0].(*age.X25519Identity)
	if !ok {
		return nil, fmt.Errorf("parsed identity is not X25519")
	}
	return id, nil
}

// ParseRecipient parses an age public key string into a Recipient.
func ParseRecipient(publicKey string) (*age.X25519Recipient, error) {
	r, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return nil, fmt.Errorf("parsing recipient: %w", err)
	}
	return r, nil
}

// EncryptWithPassphrase encrypts data using a passphrase (for key backup).
func EncryptWithPassphrase(plaintext []byte, passphrase string) ([]byte, error) {
	var buf bytes.Buffer
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("creating scrypt recipient: %w", err)
	}
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, fmt.Errorf("creating age writer: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing age writer: %w", err)
	}
	return buf.Bytes(), nil
}

// DecryptWithPassphrase decrypts data encrypted with a passphrase.
func DecryptWithPassphrase(ciphertext []byte, passphrase string) ([]byte, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("creating scrypt identity: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
	if err != nil {
		return nil, fmt.Errorf("decrypting with passphrase: %w", err)
	}
	return io.ReadAll(r)
}
