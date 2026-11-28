package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// legacyVSCodeEntry is one entry as the projected VS Code hook file wrote it.
func legacyVSCodeEntry(kind, tag string) string {
	return `{"type":"command","command":"command -v git-attributed >/dev/null 2>&1 || exit 0; git-attributed checkpoint portable-vscode --type ` +
		kind + ` --hook-input stdin # git-attributed-hook:vscode-` + tag +
		`","windows":"& 'git-attributed' 'checkpoint' 'portable-vscode'","matcher":"apply_patch","cwd":".","timeout":30}`
}

func writeLegacyVSCode(t *testing.T, root, extraPre string) string {
	t.Helper()
	path := legacyVSCodePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := legacyVSCodeEntry("human", "pre-edit") + "," + legacyVSCodeEntry("human", "shell-pre")
	if extraPre != "" {
		pre += "," + extraPre
	}
	post := legacyVSCodeEntry("ai", "post-edit") + "," + legacyVSCodeEntry("ai", "shell-post")
	content := `{"hooks":{"PreToolUse":[` + pre + `],"PostToolUse":[` + post + `]}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCopilotInstallRemovesTheLegacyVSCodeFile(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	legacy := writeLegacyVSCode(t, root, "")
	if found, err := LegacyVSCodeHooks(root); err != nil || found != legacy {
		t.Fatalf("LegacyVSCodeHooks = %q, %v; want %q", found, err, legacy)
	}
	result, err := Install(root, Options{Agent: "copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Changed, legacy) {
		t.Fatalf("Install changed = %v, want %s", result.Changed, legacy)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy file still exists: %v", err)
	}
	if found, err := LegacyVSCodeHooks(root); err != nil || found != "" {
		t.Fatalf("LegacyVSCodeHooks after install = %q, %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "hooks", "git-attributed.json")); err != nil {
		t.Fatalf("copilot hook file missing: %v", err)
	}
}

func TestCopilotInstallKeepsOtherLegacyVSCodeEntries(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	own := `{"type":"command","command":"./lint.sh","matcher":"apply_patch"}`
	legacy := writeLegacyVSCode(t, root, own)
	if _, err := Install(root, Options{Agent: "copilot"}); err != nil {
		t.Fatal(err)
	}
	config := readObject(t, legacy)
	events := config["hooks"].(map[string]any)
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 || pre[0].(map[string]any)["command"] != "./lint.sh" {
		t.Fatalf("PreToolUse after migration = %v, want only ./lint.sh", pre)
	}
	if _, ok := events["PostToolUse"]; ok {
		t.Fatalf("PostToolUse kept: %v", events)
	}
	if _, err := os.Stat(legacy + ".git-attributed.bak"); err != nil {
		matches, _ := filepath.Glob(legacy + "*")
		t.Fatalf("no backup of the rewritten legacy file: %v (have %v)", err, matches)
	}
}

func TestCopilotUninstallAlsoRemovesLegacyVSCodeEntries(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	legacy := writeLegacyVSCode(t, root, "")
	if _, err := Uninstall(root, Options{Agent: "copilot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy file still exists after uninstall: %v", err)
	}
}

func TestLegacyVSCodeHooksIgnoresUnrelatedFiles(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	if found, err := LegacyVSCodeHooks(root); err != nil || found != "" {
		t.Fatalf("no file: %q, %v", found, err)
	}
	path := legacyVSCodePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"./lint.sh"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := LegacyVSCodeHooks(root); err != nil || found != "" {
		t.Fatalf("unrelated file: %q, %v", found, err)
	}
	if _, err := Install(root, Options{Agent: "copilot"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != `{"hooks":{"PreToolUse":[{"type":"command","command":"./lint.sh"}]}}` {
		t.Fatalf("unrelated file changed: %q, %v", data, err)
	}
}
