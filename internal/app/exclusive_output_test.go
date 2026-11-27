package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExclusiveOutputParentFailures covers an export output path whose
// parent exists as a regular file.
func TestExclusiveOutputParentFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	parent := filepath.Join(root, "parent-file")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, _, err := appRun(root, zeroTime(), nil, "export", "--format", "agent-trace", "--output", filepath.Join(parent, "trace.json")); code != ExitFailure || err == nil {
		t.Fatalf("export output under file parent = %d, %v", code, err)
	}
}
