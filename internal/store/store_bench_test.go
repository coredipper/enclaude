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
	// Create test environment
	tmpDir, err := os.MkdirTemp("", "enclaude-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")

	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(sealDir, 0755)

	// Generate 1000 files, 100KB each to simulate real data
	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644)
	}

	// Create manifest
	manifest := NewManifest("test-device")
	manifest.Save(sealDir)

	cfg := &config.Config{
		Seal: config.SealSection{
			ClaudeDir: claudeDir,
			SealDir:   sealDir,
			DeviceID:  "test-device",
		},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Status(cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnsealStatus(b *testing.B) {
	// Create test environment
	tmpDir, err := os.MkdirTemp("", "enclaude-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")

	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(sealDir, 0755)

	// Generate 1000 files, 100KB each
	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644)
	}

	// Create manifest
	manifest := NewManifest("test-device")
	manifest.Save(sealDir)

	cfg := &config.Config{
		Seal: config.SealSection{
			ClaudeDir: claudeDir,
			SealDir:   sealDir,
			DeviceID:  "test-device",
		},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := UnsealStatus(cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
