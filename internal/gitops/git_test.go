package gitops

import (
	"os"
	"testing"
)

func TestRemoteAddValidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gitops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	g := New(tempDir)
	if err := g.Init(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"origin", "https://github.com/foo/bar", false},
		{"origin", "git@github.com:foo/bar.git", false},
		{"origin", "/local/path/to/repo", false},
		{"-oProxyCommand=sh", "https://github.com/foo/bar", true},
		{"--upload-pack=touch", "https://github.com/foo/bar", true},
		{"origin", "-oProxyCommand=sh", true},
		{"origin", "--upload-pack=touch", true},
		{"origin", "ext::sh -c 'touch pwned'", true},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_"+tt.url, func(t *testing.T) {
			err := g.RemoteAdd(tt.name, tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("RemoteAdd(%q, %q) expected error, got nil", tt.name, tt.url)
				}
			} else {
				if err != nil {
					t.Errorf("RemoteAdd(%q, %q) unexpected error: %v", tt.name, tt.url, err)
				} else {
					// Clean up the remote so we can add more if needed
					g.RemoteRemove(tt.name)
				}
			}
		})
	}
}
