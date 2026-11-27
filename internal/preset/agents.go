package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amichne/attributed/internal/model"
)

// DeniesOnHookError reports whether the agent behind a preset denies the tool
// call when its pre-tool hook exits non-zero. GitHub Copilot does, so its
// checkpoints report failures on stderr and still exit 0: attribution must
// never block an edit.
func DeniesOnHookError(name string) bool {
	return name == "copilot" || name == "portable-copilot"
}

// Agents lists the agents whose hooks install-hooks manages, in install order.
var Agents = []string{"claude", "droid", "codex", "copilot", "devin"}

// codexPayload is a Codex PreToolUse or PostToolUse hook input.
type codexPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	Model          string          `json:"model"`
	ToolName       string          `json:"tool_name"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolInput      json.RawMessage `json:"tool_input"`
}

// parseCodex reads Codex hook input. Codex names its shell tool Bash and its
// file tool apply_patch, whose patch text arrives in tool_input.command.
func parseCodex(explicit model.Author, data []byte) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload codexPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode codex hook input: %w", err)
	}
	if payload.ToolName == "Bash" {
		event, handled, err := shellEvent("codex", explicit, payload.Model, payload.SessionID, payload.ToolUseID)
		if err == nil && handled {
			event.TranscriptPath = payload.TranscriptPath
		}
		return event, handled, err
	}
	if !applyPatchTool(payload.ToolName) {
		return Event{}, false, nil
	}
	var input struct {
		Command string `json:"command"`
		Patch   string `json:"patch"`
		Input   string `json:"input"`
	}
	if err := decodeToolArguments(payload.ToolInput, &input); err != nil {
		return Event{}, false, err
	}
	paths, err := patchPaths(firstValue(input.Command, input.Patch, input.Input))
	if err != nil {
		return Event{}, false, err
	}
	return editEvent("codex", explicit, editFacts{
		paths:          resolveAgainst(payload.Cwd, paths),
		model:          payload.Model,
		session:        payload.SessionID,
		eventID:        payload.ToolUseID,
		transcriptPath: payload.TranscriptPath,
	})
}

// copilotPayload is a GitHub Copilot preToolUse or postToolUse hook input in
// its native camelCase format.
type copilotPayload struct {
	SessionID  string          `json:"sessionId"`
	Cwd        string          `json:"cwd"`
	ToolName   string          `json:"toolName"`
	ToolCallID string          `json:"toolCallId"`
	ToolArgs   json.RawMessage `json:"toolArgs"`
}

// parseCopilot reads Copilot hook input. Copilot sends no model, so AI
// events carry the fallback model.
func parseCopilot(explicit model.Author, data []byte) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload copilotPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode copilot hook input: %w", err)
	}
	tool := strings.ToLower(payload.ToolName)
	switch tool {
	case "bash", "powershell":
		return shellEvent("copilot", explicit, "", payload.SessionID, payload.ToolCallID)
	case "apply_patch":
		patch, err := copilotPatch(payload.ToolArgs)
		if err != nil {
			return Event{}, false, err
		}
		paths, err := patchPaths(patch)
		if err != nil {
			return Event{}, false, err
		}
		return editEvent("copilot", explicit, editFacts{
			paths: resolveAgainst(payload.Cwd, paths), session: payload.SessionID, eventID: payload.ToolCallID,
		})
	case "create", "edit", "write", "str_replace_editor":
		var input fileArguments
		if err := decodeToolArguments(payload.ToolArgs, &input); err != nil {
			return Event{}, false, err
		}
		path := input.path()
		if path == "" {
			return Event{}, false, fmt.Errorf("copilot %s arguments contain no file path", tool)
		}
		return editEvent("copilot", explicit, editFacts{
			paths: resolveAgainst(payload.Cwd, []string{path}), session: payload.SessionID, eventID: payload.ToolCallID,
		})
	default:
		return Event{}, false, nil
	}
}

// copilotPatch returns the patch body of an apply_patch call, which Copilot
// sends as a bare string.
func copilotPatch(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", errors.New("patch is missing")
	}
	if trimmed[0] == '"' {
		var patch string
		if err := json.Unmarshal(trimmed, &patch); err != nil {
			return "", fmt.Errorf("decode apply_patch arguments: %w", err)
		}
		return patch, nil
	}
	var input struct {
		Patch   string `json:"patch"`
		Input   string `json:"input"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(trimmed, &input); err != nil {
		return "", fmt.Errorf("decode apply_patch arguments: %w", err)
	}
	return firstValue(input.Patch, input.Input, input.Command), nil
}

// devinPayload is a Devin CLI PreToolUse or PostToolUse hook input. Devin
// documents hook_event_name, tool_name, tool_input, session_id, and
// prompt_id; the remaining fields are read when present.
type devinPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	Model          string          `json:"model"`
	ToolName       string          `json:"tool_name"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolInput      json.RawMessage `json:"tool_input"`
}

// parseDevin reads Devin CLI hook input. Devin's file tools are edit, write,
// apply_patch, and notebook_edit; its shell tool is exec.
func parseDevin(explicit model.Author, data []byte) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload devinPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode devin hook input: %w", err)
	}
	facts := editFacts{
		model:          payload.Model,
		session:        payload.SessionID,
		eventID:        payload.ToolUseID,
		transcriptPath: payload.TranscriptPath,
	}
	switch payload.ToolName {
	case "exec":
		event, handled, err := shellEvent("devin", explicit, payload.Model, payload.SessionID, payload.ToolUseID)
		if err == nil && handled {
			event.TranscriptPath = payload.TranscriptPath
		}
		return event, handled, err
	case "apply_patch":
		var input struct {
			Patch   string `json:"patch"`
			Input   string `json:"input"`
			Command string `json:"command"`
		}
		if err := decodeToolArguments(payload.ToolInput, &input); err != nil {
			return Event{}, false, err
		}
		paths, err := patchPaths(firstValue(input.Patch, input.Input, input.Command))
		if err != nil {
			return Event{}, false, err
		}
		facts.paths = resolveAgainst(payload.Cwd, paths)
		return editEvent("devin", explicit, facts)
	case "edit", "write", "notebook_edit":
		var input fileArguments
		if err := decodeToolArguments(payload.ToolInput, &input); err != nil {
			return Event{}, false, err
		}
		path := input.path()
		if path == "" {
			return Event{}, false, fmt.Errorf("devin %s input contains no file path", payload.ToolName)
		}
		facts.paths = resolveAgainst(payload.Cwd, []string{path})
		return editEvent("devin", explicit, facts)
	default:
		return Event{}, false, nil
	}
}

// fileArguments holds the file path spellings agent file tools use.
type fileArguments struct {
	FilePath      string `json:"file_path"`
	FilePathCamel string `json:"filePath"`
	Path          string `json:"path"`
	NotebookPath  string `json:"notebook_path"`
}

func (arguments fileArguments) path() string {
	return firstValue(arguments.FilePath, arguments.FilePathCamel, arguments.Path, arguments.NotebookPath)
}

// decodeToolArguments decodes a tool argument object, which some agents send
// as a JSON-encoded string.
func decodeToolArguments(raw json.RawMessage, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("tool input is missing")
	}
	if trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return fmt.Errorf("decode tool input string: %w", err)
		}
		trimmed = []byte(encoded)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("decode tool input: %w", err)
	}
	return nil
}

// editFacts are the parts of an edit event an agent payload supplies.
type editFacts struct {
	paths          []string
	model          string
	session        string
	eventID        string
	transcriptPath string
}

// editEvent builds and validates one edit checkpoint for agent.
func editEvent(agent string, explicit model.Author, facts editFacts) (Event, bool, error) {
	paths := uniquePaths(facts.paths)
	if len(paths) == 0 {
		return Event{}, false, errors.New("hook input contains no usable file path")
	}
	event := Event{
		Kind:           model.CheckpointKindEdit,
		Type:           explicit,
		Paths:          paths,
		EventID:        facts.eventID,
		TranscriptPath: facts.transcriptPath,
	}
	if explicit != model.AuthorAI {
		return event, true, nil
	}
	event.Agent = agent
	event.Model = firstValue(facts.model, FallbackModel)
	event.Session = facts.session
	if err := model.ValidateAttribution(model.Attribution{
		Author: event.Type, Agent: event.Agent, Model: event.Model, Session: event.Session,
	}); err != nil {
		return Event{}, false, err
	}
	return event, true, nil
}

// resolveAgainst makes relative paths absolute against the agent's working
// directory. Agents report patch paths relative to their session directory,
// which can be a subdirectory of the repository.
func resolveAgainst(cwd string, paths []string) []string {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return paths
	}
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		native := filepath.FromSlash(path)
		if path == "" || filepath.IsAbs(native) {
			resolved = append(resolved, path)
			continue
		}
		resolved = append(resolved, filepath.Join(cwd, native))
	}
	return resolved
}

// fromClaudeCode reports whether a Claude-format payload came from Claude
// Code itself. GitHub Copilot and Devin also run hooks from
// .claude/settings.json; Claude Code always sends transcript_path, and those
// agents do not.
func fromClaudeCode(data []byte) (bool, error) {
	var probe struct {
		TranscriptPath *json.RawMessage `json:"transcript_path"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("decode claude hook input: %w", err)
	}
	return probe.TranscriptPath != nil, nil
}
