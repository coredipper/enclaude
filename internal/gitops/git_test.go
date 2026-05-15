package gitops

import (
	"os"
	"strings"
	"testing"
)

func TestConfigMergeDriverQuoting(t *testing.T) {
	tmp := t.TempDir()
	g := New(tmp)
	if err := g.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// This is a test for the quoting logic inside ConfigMergeDriver.
	// Since os.Executable() is used, the driver string will start with the test executable path.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	testArgs := []string{"merge-driver", "manifest", "has'quote", "%O", "%A", "%B"}
	if err := g.ConfigMergeDriver("testdriver", testArgs); err != nil {
		t.Fatalf("ConfigMergeDriver failed: %v", err)
	}

	// Check what was set in git config
	out, err := g.run("config", "merge.testdriver.driver")
	if err != nil {
		t.Fatalf("failed to read git config: %v", err)
	}
	out = strings.TrimSpace(out)

	// Validate quoting
	expectedExe := "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
	if !strings.HasPrefix(out, expectedExe) {
		t.Errorf("expected driver to start with %s, got %s", expectedExe, out)
	}

	if !strings.Contains(out, "'merge-driver'") {
		t.Errorf("expected 'merge-driver' to be properly quoted, got %s", out)
	}

	if !strings.Contains(out, "'has'\\''quote'") {
		t.Errorf("expected 'has'\\''quote' to be properly quoted, got %s", out)
	}

	if !strings.Contains(out, " %O %A %B") {
		t.Errorf("expected git special tokens to remain unquoted, got %s", out)
	}
}
