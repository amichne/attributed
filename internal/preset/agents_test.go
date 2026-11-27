package preset

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amichne/attributed/internal/model"
)

func captured(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "captured", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type agentCase struct {
	name    string
	preset  string
	author  model.Author
	payload string
	want    Event
	skipped bool
}

func checkAgentCase(t *testing.T, test agentCase) {
	t.Helper()
	event, handled, err := Parse(test.preset, test.author, strings.NewReader(test.payload))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if test.skipped {
		if handled {
			t.Fatalf("Parse() handled %+v, want it skipped", event)
		}
		return
	}
	if !handled {
		t.Fatal("Parse() skipped the payload, want it handled")
	}
	if !reflect.DeepEqual(event, test.want) {
		t.Fatalf("Parse() = %+v\nwant      %+v", event, test.want)
	}
}

func TestParseCapturedCodexHooks(t *testing.T) {
	t.Parallel()
	transcript := "/workspace/codex-home/sessions/2026/11/27/rollout-2026-11-27T17-55-43-01a21f38-c23f-7682-b876-3cdc51098d40.jsonl"
	session := "01a21f38-c23f-7682-b876-3cdc51098d40"
	for _, test := range []agentCase{{
		name: "apply_patch add, before", preset: "codex", author: model.AuthorHuman,
		payload: captured(t, "codex-apply-patch-add-pre.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths:   []string{"/workspace/repo/hello.txt"},
			EventID: "exec-4a451f66-b830-42cb-9ada-de212779eea6", TranscriptPath: transcript,
		},
	}, {
		name: "apply_patch update, after", preset: "codex", author: model.AuthorAI,
		payload: captured(t, "codex-apply-patch-update-post.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorAI,
			Agent: "codex", Model: "gpt-6.2-sol", Session: session,
			Paths:   []string{"/workspace/repo/hello.txt"},
			EventID: "exec-7cc748ce-a976-49df-8440-f9a2f288a5d2", TranscriptPath: transcript,
		},
	}, {
		name: "Bash, after", preset: "codex", author: model.AuthorAI,
		payload: captured(t, "codex-bash-post.json"),
		want: Event{
			Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
			Agent: "codex", Model: "gpt-6.2-sol", Session: session,
			EventID: "exec-4aa6affc-92de-49ba-8401-472b23e60c26", TranscriptPath: transcript,
		},
	}, {
		name: "the legacy portable preset reads the patch too", preset: "portable-codex", author: model.AuthorHuman,
		payload: captured(t, "codex-apply-patch-add-pre.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths:   []string{"hello.txt"},
			EventID: "exec-4a451f66-b830-42cb-9ada-de212779eea6", TranscriptPath: transcript,
		},
	}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkAgentCase(t, test)
		})
	}
}

func TestParseCapturedCopilotHooks(t *testing.T) {
	t.Parallel()
	session := "04f7f19b-14ac-48c9-a645-0630a6190e5d"
	for _, test := range []agentCase{{
		name: "apply_patch with a bare patch string, after", preset: "copilot", author: model.AuthorAI,
		payload: captured(t, "copilot-apply-patch-pre.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorAI,
			Agent: "copilot", Model: FallbackModel, Session: session,
			Paths:   []string{"/workspace/repo/hello.txt"},
			EventID: "custom_call_x072qHqob0ZBgC645YSAujN6",
		},
	}, {
		name: "bash, after", preset: "copilot", author: model.AuthorAI,
		payload: captured(t, "copilot-bash-post.json"),
		want: Event{
			Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
			Agent: "copilot", Model: FallbackModel, Session: session,
			EventID: "call_6yNn6L2Dtt0qHBv6ZV2O6msi",
		},
	}, {
		name: "bash, before", preset: "copilot", author: model.AuthorHuman,
		payload: captured(t, "copilot-bash-post.json"),
		want: Event{
			Kind: model.CheckpointKindShellPre, Type: model.AuthorHuman,
			EventID: "call_6yNn6L2Dtt0qHBv6ZV2O6msi",
		},
	}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkAgentCase(t, test)
		})
	}
}

func TestParseCopilotFileTools(t *testing.T) {
	t.Parallel()
	for _, test := range []agentCase{{
		name: "create with path", preset: "copilot", author: model.AuthorHuman,
		payload: `{"sessionId":"s","cwd":"/workspace/repo/sub","toolCallId":"c1","toolName":"create","toolArgs":{"path":"notes.md","file_text":"x"}}`,
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths: []string{"/workspace/repo/sub/notes.md"}, EventID: "c1",
		},
	}, {
		name: "edit with an encoded argument object", preset: "copilot", author: model.AuthorHuman,
		payload: `{"sessionId":"s","cwd":"/workspace/repo","toolCallId":"c2","toolName":"edit","toolArgs":"{\"path\":\"/workspace/repo/a.go\"}"}`,
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths: []string{"/workspace/repo/a.go"}, EventID: "c2",
		},
	}, {
		name: "view is not an edit", preset: "copilot", author: model.AuthorAI,
		payload: `{"sessionId":"s","toolName":"view","toolArgs":{"path":"a.go"}}`,
		skipped: true,
	}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkAgentCase(t, test)
		})
	}
}

func TestParseCopilotRejectsUnusableArguments(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"edit without a path":   `{"toolName":"edit","toolArgs":{"content":"x"}}`,
		"apply_patch with text": `{"toolName":"apply_patch","toolArgs":"not a patch"}`,
		"apply_patch missing":   `{"toolName":"apply_patch"}`,
		"unsafe call id":        `{"toolName":"bash","toolCallId":"bad\nid"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := Parse("copilot", model.AuthorAI, strings.NewReader(payload)); err == nil {
				t.Fatal("Parse() accepted the payload")
			}
		})
	}
}

func TestParseDevinHooks(t *testing.T) {
	t.Parallel()
	for _, test := range []agentCase{{
		name: "write", preset: "devin", author: model.AuthorAI,
		payload: `{"hook_event_name":"PostToolUse","tool_name":"write","tool_input":{"file_path":"/workspace/repo/a.go","content":"x"},"session_id":"3f8d1c2a","prompt_id":"b71e9d40"}`,
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorAI,
			Agent: "devin", Model: FallbackModel, Session: "3f8d1c2a",
			Paths: []string{"/workspace/repo/a.go"},
		},
	}, {
		name: "apply_patch", preset: "devin", author: model.AuthorHuman,
		payload: `{"hook_event_name":"PreToolUse","tool_name":"apply_patch","tool_input":{"patch":"*** Begin Patch\n*** Update File: a.go\n*** End Patch"},"cwd":"/workspace/repo","session_id":"3f8d1c2a"}`,
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths: []string{"/workspace/repo/a.go"},
		},
	}, {
		name: "exec with a call id", preset: "devin", author: model.AuthorAI,
		payload: `{"hook_event_name":"PostToolUse","tool_name":"exec","tool_input":{"command":"ls"},"tool_use_id":"t1","session_id":"3f8d1c2a"}`,
		want: Event{
			Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
			Agent: "devin", Model: FallbackModel, Session: "3f8d1c2a", EventID: "t1",
		},
	}, {
		name: "exec as documented, without a call id", preset: "devin", author: model.AuthorHuman,
		payload: `{"hook_event_name":"PreToolUse","tool_name":"exec","tool_input":{"command":"ls"},"session_id":"3f8d1c2a","prompt_id":"b71e9d40"}`,
		want:    Event{Kind: model.CheckpointKindShellPre, Type: model.AuthorHuman},
	}, {
		name: "read is not an edit", preset: "devin", author: model.AuthorAI,
		payload: `{"tool_name":"read","tool_input":{"file_path":"a.go"}}`,
		skipped: true,
	}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkAgentCase(t, test)
		})
	}
}

func TestClaudePresetSkipsOtherAgents(t *testing.T) {
	t.Parallel()
	session := "8350e5ca-84f6-4835-a09e-e9fb2ca6148b"
	transcript := "/home/user/.claude/projects/-workspace-repo/8350e5ca-84f6-4835-a09e-e9fb2ca6148b.jsonl"
	for _, test := range []agentCase{{
		name: "Claude Code write", preset: "claude", author: model.AuthorHuman,
		payload: captured(t, "claude-write-pre.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorHuman,
			Paths:   []string{"/workspace/repo/hello.txt"},
			EventID: "toolu_01Ekjm1cQeyszPsinpyvaKgq", TranscriptPath: transcript,
		},
	}, {
		name: "Claude Code edit", preset: "claude", author: model.AuthorAI,
		payload: captured(t, "claude-edit-post.json"),
		want: Event{
			Kind: model.CheckpointKindEdit, Type: model.AuthorAI,
			Agent: "claude", Model: FallbackModel, Session: session,
			Paths:   []string{"/workspace/repo/hello.txt"},
			EventID: "toolu_01THvtLdKk4XHTibQVv5kuwW", TranscriptPath: transcript,
		},
	}, {
		name: "Copilot running Claude Code's edit hook", preset: "claude", author: model.AuthorHuman,
		payload: captured(t, "copilot-claude-format-edit-pre.json"),
		skipped: true,
	}, {
		name: "Copilot running Claude Code's shell hook", preset: "claude", author: model.AuthorAI,
		payload: captured(t, "copilot-claude-format-bash-pre.json"),
		skipped: true,
	}, {
		name: "Devin running Claude Code's hook", preset: "claude", author: model.AuthorAI,
		payload: `{"hook_event_name":"PreToolUse","tool_name":"edit","tool_input":{"file_path":"a.go"},"session_id":"3f8d1c2a","prompt_id":"b71e9d40"}`,
		skipped: true,
	}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkAgentCase(t, test)
		})
	}
}

func TestDeniesOnHookError(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"copilot": true, "portable-copilot": true, "portable-vscode": true,
		"claude": false, "droid": false, "codex": false, "devin": false,
	} {
		if got := DeniesOnHookError(name); got != want {
			t.Errorf("DeniesOnHookError(%q) = %t, want %t", name, got, want)
		}
	}
}
