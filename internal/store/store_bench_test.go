package store

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coredipper/enclaude/internal/config"
)

func BenchmarkStatus(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "enclaude-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		b.Fatalf("os.MkdirAll: %v", err)
	}
	if err := os.MkdirAll(sealDir, 0755); err != nil {
		b.Fatalf("os.MkdirAll: %v", err)
	}

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		if _, err := rand.Read(content); err != nil {
			b.Fatalf("failed to generate random data: %v", err)
		}
		if err := os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644); err != nil {
			b.Fatalf("os.WriteFile: %v", err)
		}
	}

	manifest := NewManifest("test-device")
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		info, _ := os.Stat(filepath.Join(claudeDir, path))
		manifest.Files[path] = FileEntry{
			ContentHash:   "mock-hash",
			SizePlaintext: info.Size(),
			Mtime:         info.ModTime().UTC().Format(time.RFC3339),
			ModTimeNs:     info.ModTime().UnixNano(),
		}
	}
	if err := manifest.Save(sealDir); err != nil {
		b.Fatalf("manifest.Save: %v", err)
	}

	cfg := &config.Config{
		Seal:    config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test-device"},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Status(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnsealStatus(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "enclaude-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		b.Fatalf("os.MkdirAll: %v", err)
	}
	if err := os.MkdirAll(sealDir, 0755); err != nil {
		b.Fatalf("os.MkdirAll: %v", err)
	}

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		if _, err := rand.Read(content); err != nil {
			b.Fatalf("failed to generate random data: %v", err)
		}
		if err := os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644); err != nil {
			b.Fatalf("os.WriteFile: %v", err)
		}
	}

	manifest := NewManifest("test-device")
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		info, _ := os.Stat(filepath.Join(claudeDir, path))
		manifest.Files[path] = FileEntry{
			ContentHash:   "mock-hash",
			SizePlaintext: info.Size(),
			Mtime:         info.ModTime().UTC().Format(time.RFC3339),
			ModTimeNs:     info.ModTime().UnixNano(),
		}
	}
	if err := manifest.Save(sealDir); err != nil {
		b.Fatalf("manifest.Save: %v", err)
	}

	cfg := &config.Config{
		Seal:    config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test-device"},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := UnsealStatus(cfg); err != nil {
			b.Fatal(err)
		}
	}
}
