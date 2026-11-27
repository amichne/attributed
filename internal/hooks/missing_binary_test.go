package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGitHookBlocksSkipQuietlyWithoutTheBinary runs every managed Git hook
// block against an executable that does not exist. Each block must exit 0
// with no output, and user code after the block must still run.
func TestGitHookBlocksSkipQuietlyWithoutTheBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell hooks")
	}
	missing := filepath.Join(t.TempDir(), "missing", "git-attributed")
	blocks := map[string]string{
		"post-commit":           postCommitCommand(missing),
		"pre-push":              notesPushCommand(missing),
		"post-rewrite":          rewriteHookCommand(missing, "post-rewrite", true),
		"post-merge":            postMergeCommand(missing),
		"post-checkout":         rewriteHookCommand(missing, "post-checkout", false),
		"reference-transaction": referenceTransactionHookCommand(missing),
	}
	root := hookRepo(t)
	for name, command := range blocks {
		script := "#!/bin/sh\n" + blockStart + "\n" + guardedGitHookCommand(missing, command) + "\n" +
			blockEnd + "\necho user-code-ran\n"
		run := exec.Command("sh", "-c", script, name, "committed", "origin")
		run.Dir = root
		run.Stdin = strings.NewReader("")
		out, err := run.CombinedOutput()
		if err != nil || string(out) != "user-code-ran\n" {
			t.Errorf("%s without the binary = %q, %v; want only the user output", name, out, err)
		}
	}
}

func TestInstallWritesGuardedHooks(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	if _, err := Install(root, Options{Agent: "claude", Git: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post-commit", "pre-push", "post-rewrite", "post-merge", "post-checkout", "reference-transaction"} {
		hook := string(mustRead(t, filepath.Join(root, ".git", "hooks", name)))
		if !strings.Contains(hook, blockStart+"\nif command -v '") || !strings.Contains(hook, "\nfi\n"+blockEnd) {
			t.Errorf("%s is not guarded:\n%s", name, hook)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for event, entries := range config.Hooks {
		for _, entry := range entries {
			for _, hook := range entry.Hooks {
				if !strings.HasPrefix(hook.Command, "command -v 'git-attributed' >/dev/null 2>&1 || exit 0; 'git-attributed' checkpoint claude ") {
					t.Errorf("%s command is not guarded: %q", event, hook.Command)
				}
			}
		}
	}
}

func TestUnguardedGitHookBlockIsUpgradedAndRemoved(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".git", "hooks", "post-commit")
	legacy := "#!/bin/sh\n" + blockStart + "\n" + postCommitCommand("/old/git-attributed") + "\n" + blockEnd +
		"\necho mine\n"
	if err := os.WriteFile(path, []byte(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	upgraded := string(mustRead(t, path))
	if !strings.Contains(upgraded, "if command -v '") || strings.Contains(upgraded, "/old/git-attributed") ||
		!strings.HasSuffix(upgraded, "echo mine\n") {
		t.Fatalf("upgraded hook:\n%s", upgraded)
	}
	if _, err := Uninstall(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	if after := string(mustRead(t, path)); after != "#!/bin/sh\necho mine\n" {
		t.Fatalf("uninstall left %q", after)
	}
}

func TestUnguardedAgentHookIsUpgradedAndRemoved(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	current := agentSpecs("claude", productName)["PostToolUse"][0]
	legacy := cutAgentGuard(current.command)
	if legacy == current.command {
		t.Fatal("current agent command carries no guard")
	}
	data, err := json.Marshal(map[string]any{"hooks": map[string]any{
		"PostToolUse": []any{map[string]any{
			"matcher": current.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": legacy, "timeout": 30}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	got := readObject(t, path)
	upgraded := got["hooks"].(map[string]any)["PostToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if upgraded != current.command {
		t.Fatalf("agent hook = %q, want %q", upgraded, current.command)
	}
	if _, err := Uninstall(root, Options{Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, path)); strings.Contains(got, "git-attributed") {
		t.Fatalf("uninstall left a managed agent hook:\n%s", got)
	}
}

func TestGuardsOnlyRecognizeGitAttributedExecutables(t *testing.T) {
	t.Parallel()
	foreign := "command -v 'other-tool' >/dev/null 2>&1 || exit 0; 'git-attributed' checkpoint claude"
	if cutAgentGuard(foreign) != foreign {
		t.Fatal("an agent guard for another executable was accepted")
	}
	foreignBlock := "if command -v 'other-tool' >/dev/null 2>&1; then\n'git-attributed' annotate || exit 1\nfi"
	if cutGitHookGuard(foreignBlock) != foreignBlock {
		t.Fatal("a Git hook guard for another executable was accepted")
	}
}
