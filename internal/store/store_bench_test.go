package store

import (
	"fmt"
	"math/rand"
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
	defer os.RemoveAll(tmpDir)

	claudeDir := filepath.Join(tmpDir, "claude")
	sealDir := filepath.Join(tmpDir, "seal")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(sealDir, 0755)

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644)
	}

	manifest := NewManifest("test-device")
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		info, _ := os.Stat(filepath.Join(claudeDir, path))
		manifest.Files[path] = FileEntry{
			ContentHash:   "mock-hash",
			SizePlaintext: info.Size(),
			Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
			ModTimeMs:     info.ModTime().UnixMilli(),
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

	for i := 0; i < 1000; i++ {
		content := make([]byte, 100*1024)
		rand.Read(content)
		os.WriteFile(filepath.Join(claudeDir, fmt.Sprintf("file-%d.txt", i)), content, 0644)
	}

	manifest := NewManifest("test-device")
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		info, _ := os.Stat(filepath.Join(claudeDir, path))
		manifest.Files[path] = FileEntry{
			ContentHash:   "mock-hash",
			SizePlaintext: info.Size(),
			Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
			ModTimeMs:     info.ModTime().UnixMilli(),
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
