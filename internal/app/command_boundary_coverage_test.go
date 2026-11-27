package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExportHeadReadFailure(t *testing.T) {
	root := t.TempDir()
	configureFakeCommandGit(t, root, fakeExportGitScript, map[string]string{
		"FAKE_EXPORT_ROOT": root,
	})
	code, _, _, err := appRun(root, zeroTime(), nil, "export", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("export HEAD failure = %d, %v", code, err)
	}
}

func TestCheckpointDiscoverNonRepositoryFailure(t *testing.T) {
	root := t.TempDir()
	configureFakeCommandGit(t, root, fakeCheckpointGitScript, map[string]string{
		"FAKE_CHECKPOINT_ROOT": root,
	})
	payload := `{"session_id":"s","tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(payload),
		"checkpoint", "droid", "--type", "human", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("checkpoint discover failure = %d, %v", code, err)
	}
}

func configureFakeCommandGit(t *testing.T, root, script string, values map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	path := filepath.Join(root, "git")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const fakeExportGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_EXPORT_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_EXPORT_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_EXPORT_ROOT" ;;
  *"rev-parse --verify HEAD"*) exit 2 ;;
  *) exit 1 ;;
esac
`

const fakeCheckpointGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_CHECKPOINT_ROOT" ;;
  *) exit 2 ;;
esac
`
