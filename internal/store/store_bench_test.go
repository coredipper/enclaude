package store

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
)

func BenchmarkStatus(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "enclaude-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(sealDir, 0755)

	manifest := NewManifest("test-device")

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		path := filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i))
		os.WriteFile(path, content, 0644)
		info, _ := os.Stat(path)

		manifest.Files[fmt.Sprintf("file-%d.txt", i)] = FileEntry{
			ContentHash:   ContentHash(content),
			SizePlaintext: info.Size(),
			Mtime:         info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	manifest.Save(sealDir)

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
	defer os.RemoveAll(tmpDir)

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(sealDir, 0755)

	manifest := NewManifest("test-device")

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		path := filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i))
		os.WriteFile(path, content, 0644)
		info, _ := os.Stat(path)

		manifest.Files[fmt.Sprintf("file-%d.txt", i)] = FileEntry{
			ContentHash:   ContentHash(content),
			SizePlaintext: info.Size(),
			Mtime:         info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	manifest.Save(sealDir)

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
