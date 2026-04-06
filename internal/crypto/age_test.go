package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	id, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	if id.Recipient() == nil {
		t.Fatal("generated key has no recipient")
	}
	if id.String() == "" {
		t.Fatal("generated key has empty string representation")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	id, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello world")},
		{"jsonl line", []byte(`{"display":"test prompt","timestamp":1760474511560,"project":"/Users/test/project"}` + "\n")},
		{"large", bytes.Repeat([]byte("x"), 1024*1024)}, // 1 MB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(tt.plaintext, id.Recipient())
			if err != nil {
				t.Fatalf("Encrypt() error: %v", err)
			}

			// Encrypted should differ from plaintext
			if len(tt.plaintext) > 0 && bytes.Equal(encrypted, tt.plaintext) {
				t.Fatal("encrypted data equals plaintext")
			}

			decrypted, err := Decrypt(encrypted, id)
			if err != nil {
				t.Fatalf("Decrypt() error: %v", err)
			}

			if !bytes.Equal(decrypted, tt.plaintext) {
				t.Fatalf("round-trip failed: got %d bytes, want %d bytes", len(decrypted), len(tt.plaintext))
			}
		})
	}
}

func TestEncryptDecryptWithPassphrase(t *testing.T) {
	plaintext := []byte("AGE-SECRET-KEY-1FAKE...")
	passphrase := "test-passphrase-123"

	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	if err != nil {
		t.Fatalf("EncryptWithPassphrase() error: %v", err)
	}

	decrypted, err := DecryptWithPassphrase(encrypted, passphrase)
	if err != nil {
		t.Fatalf("DecryptWithPassphrase() error: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("passphrase round-trip failed")
	}
}

func TestDecryptWithWrongPassphrase(t *testing.T) {
	plaintext := []byte("secret data")
	encrypted, err := EncryptWithPassphrase(plaintext, "correct-passphrase")
	if err != nil {
		t.Fatalf("EncryptWithPassphrase() error: %v", err)
	}

	_, err = DecryptWithPassphrase(encrypted, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error decrypting with wrong passphrase")
	}
}

func TestParseIdentity(t *testing.T) {
	id, _ := GenerateKey()
	parsed, err := ParseIdentity(id.String())
	if err != nil {
		t.Fatalf("ParseIdentity() error: %v", err)
	}
	if parsed.Recipient().String() != id.Recipient().String() {
		t.Fatal("parsed identity has different public key")
	}
}

func TestParseRecipient(t *testing.T) {
	id, _ := GenerateKey()
	r, err := ParseRecipient(id.Recipient().String())
	if err != nil {
		t.Fatalf("ParseRecipient() error: %v", err)
	}
	if r.String() != id.Recipient().String() {
		t.Fatal("parsed recipient has different public key")
	}
}
