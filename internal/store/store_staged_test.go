package store

import (
	"strings"
	"testing"
)

func TestStagedObjectPath(t *testing.T) {
	root := "/tmp/rotate-staging"
	validHash := strings.Repeat("a", 64)
	invalidHash := "../../etc/passwd"

	path, err := stagedObjectPath(root, validHash)
	if err != nil {
		t.Errorf("stagedObjectPath(..., %q) returned unexpected error: %v", validHash, err)
	}
	expectedSuffix := validHash[:2] + "/" + validHash[2:] + ".age"
	if !strings.HasSuffix(path, expectedSuffix) {
		t.Errorf("stagedObjectPath(..., %q) = %q, want suffix %q", validHash, path, expectedSuffix)
	}

	_, err = stagedObjectPath(root, invalidHash)
	if err == nil {
		t.Errorf("stagedObjectPath(..., %q) expected error for path traversal hash", invalidHash)
	}
}
