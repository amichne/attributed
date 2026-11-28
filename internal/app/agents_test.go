package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func agentPayload(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const updateFilePatch = "*** Begin Patch\n*** Update File: file.txt\n@@\n-base\n+base\n+ai\n*** End Patch"

func TestCodexCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	payload := func(event string) string {
		return agentPayload(t, map[string]any{
			"session_id": "thr_1", "cwd": root, "hook_event_name": event, "model": "gpt-6.2-sol",
			"tool_name": "apply_patch", "tool_use_id": "exec-1", "tool_input": map[string]any{"command": updateFilePatch},
		})
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	stdout := runCheckpointBlameFlow(t, root, now, "codex", payload("PreToolUse"), payload("PostToolUse"))
	if !strings.Contains(stdout, "ai:codex/gpt-6.2-sol") {
		t.Fatalf("blame %q does not attribute the line to Codex", stdout)
	}
}

func TestCopilotCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	// Copilot runs hooks in the session directory and sends the patch bare.
	payload := agentPayload(t, map[string]any{
		"sessionId": "s-1", "cwd": root, "toolName": "apply_patch", "toolCallId": "call-1", "toolArgs": updateFilePatch,
	})
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	stdout := runCheckpointBlameFlow(t, root, now, "copilot", payload, payload)
	if !strings.Contains(stdout, "ai:copilot/unknown") {
		t.Fatalf("blame %q does not attribute the line to Copilot", stdout)
	}
}

func TestDevinCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	payload := agentPayload(t, map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "edit", "session_id": "3f8d1c2a", "prompt_id": "b71e9d40",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "file.txt")},
	})
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	stdout := runCheckpointBlameFlow(t, root, now, "devin", payload, payload)
	if !strings.Contains(stdout, "ai:devin/unknown") {
		t.Fatalf("blame %q does not attribute the line to Devin", stdout)
	}
}

func TestCopilotCheckpointFailuresNeverDenyTheTool(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	broken := `{"toolName":"apply_patch","toolArgs":"not a patch"}`
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, preset := range []string{"copilot", "portable-copilot"} {
		code, _, stderr, err := appRun(root, now, strings.NewReader(broken),
			"checkpoint", preset, "--type", "human", "--hook-input", "stdin")
		if code != ExitSuccess || err != nil {
			t.Fatalf("%s checkpoint = %d, %v; Copilot would deny the tool", preset, code, err)
		}
		if !strings.HasPrefix(stderr, "git-attributed checkpoint: ") {
			t.Fatalf("%s checkpoint stderr = %q, want the parse error", preset, stderr)
		}
	}
	code, _, _, err := appRun(root, now, strings.NewReader(`{"tool_name":"apply_patch","tool_input":{"command":"not a patch"}}`),
		"checkpoint", "codex", "--type", "human", "--hook-input", "stdin")
	if code == ExitSuccess || err == nil {
		t.Fatalf("codex checkpoint = %d, %v; want the failure reported", code, err)
	}
}

func TestClaudeCheckpointIgnoresCopilotRunningItsHooks(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	// Copilot runs .claude/settings.json hooks with this payload. Before, the
	// claude preset exited 1 on it, and Copilot denied the edit.
	payload := `{"hook_event_name":"PreToolUse","session_id":"s","timestamp":"2026-11-27T22:57:03.076Z","cwd":"/workspace/repo","tool_use_id":"c","tool_name":"Edit","tool_input":"*** Begin Patch\n*** Add File: hello.txt\n+hi\n*** End Patch\n"}`
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	code, stdout, stderr, err := appRun(root, now, strings.NewReader(payload),
		"checkpoint", "claude", "--type", "human", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("claude checkpoint = %d, %q, %q, %v; want a quiet skip", code, stdout, stderr, err)
	}
}

func TestStatusWarnsAboutTheLegacyVSCodeHookFile(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	if err := os.MkdirAll(filepath.Join(root, ".github", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	appWrite(t, root, ".github/hooks/git-attributed-vscode.json",
		`{"hooks":{"PreToolUse":[{"type":"command","command":"command -v git-attributed >/dev/null 2>&1 || exit 0; git-attributed checkpoint portable-vscode --type human --hook-input stdin # git-attributed-hook:vscode-pre-edit"}]}}`)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	code, _, stderr, err := appRun(root, now, nil, "status")
	if code != ExitSuccess || err != nil || !strings.Contains(stderr, "install-hooks --agent copilot") {
		t.Fatalf("status = %d, %q, %v; want the legacy VS Code warning", code, stderr, err)
	}
	if code, _, _, err := appRun(root, now, nil, "install-hooks", "--agent", "copilot"); code != ExitSuccess || err != nil {
		t.Fatalf("install-hooks --agent copilot = %d, %v", code, err)
	}
	code, _, stderr, err = appRun(root, now, nil, "status")
	if code != ExitSuccess || err != nil || strings.Contains(stderr, "git-attributed-vscode.json") {
		t.Fatalf("status after migration = %d, %q, %v", code, stderr, err)
	}
}

func TestInstallHooksRejectsUnknownAgents(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	code, _, _, err := appRun(root, now, nil, "install-hooks", "--agent", "claude,cursor")
	if code == ExitSuccess || err == nil || !strings.Contains(err.Error(), `unsupported agent "cursor"`) {
		t.Fatalf("install-hooks --agent claude,cursor = %d, %v", code, err)
	}
}
