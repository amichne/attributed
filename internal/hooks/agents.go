package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/amichne/attributed/internal/preset"
)

// SelectAgents parses an --agent value: one agent, a comma-separated list,
// all, or none.
func SelectAgents(value string) ([]string, error) {
	switch value {
	case "all":
		return slices.Clone(preset.Agents), nil
	case "none":
		return nil, nil
	}
	var agents []string
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(preset.Agents, name) {
			return nil, fmt.Errorf("unsupported agent %q: use %s, all, or none", name, strings.Join(preset.Agents, ", "))
		}
		if !slices.Contains(agents, name) {
			agents = append(agents, name)
		}
	}
	return agents, nil
}

func selectedAgents(value string) []string {
	agents, err := SelectAgents(value)
	if err != nil {
		return nil
	}
	return agents
}

// agentFile is where and how one agent reads its hooks.
type agentFile struct {
	path string
	// nested puts the events under a top-level "hooks" key.
	nested bool
	// copilot uses Copilot's versioned format with flat entries.
	copilot bool
	// owned files belong to git-attributed alone and are removed when
	// uninstall leaves no hooks in them.
	owned bool
}

func agentConfigFile(root, agent string, user bool) (agentFile, error) {
	base := root
	if user {
		home, err := os.UserHomeDir()
		if err != nil {
			return agentFile{}, fmt.Errorf("resolve home directory: %w", err)
		}
		base = home
	}
	var file agentFile
	switch agent {
	case "droid":
		file = agentFile{path: filepath.Join(base, ".factory", "hooks.json")}
	case "claude":
		file = agentFile{path: filepath.Join(base, ".claude", "settings.json"), nested: true}
	case "codex":
		if user {
			base = homeOverride(base, "CODEX_HOME", ".codex")
		} else {
			base = filepath.Join(base, ".codex")
		}
		file = agentFile{path: filepath.Join(base, "hooks.json"), nested: true}
	case "copilot":
		if user {
			base = filepath.Join(homeOverride(base, "COPILOT_HOME", ".copilot"), "hooks")
		} else {
			base = filepath.Join(base, ".github", "hooks")
		}
		file = agentFile{path: filepath.Join(base, "git-attributed.json"), nested: true, copilot: true, owned: true}
	case "devin":
		if user {
			config := filepath.Join(base, ".config")
			if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
				config = xdg
			}
			file = agentFile{path: filepath.Join(config, "devin", "config.json"), nested: true}
			base = config
		} else {
			file = agentFile{path: filepath.Join(base, ".devin", "hooks.v1.json")}
		}
	default:
		return agentFile{}, fmt.Errorf("unsupported agent %q", agent)
	}
	if err := rejectSymlinkPath(base, filepath.Dir(file.path)); err != nil {
		return agentFile{}, err
	}
	return file, nil
}

// homeOverride returns the directory an agent's *_HOME variable names, or
// dir under home.
func homeOverride(home, variable, dir string) string {
	if value := os.Getenv(variable); filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(home, dir)
}

// agentEvents names an agent's before and after tool events and the tool
// names its hooks match.
type agentEvents struct {
	pre, post   string
	editMatcher string
	shellMatch  string
}

func eventsFor(agent string) agentEvents {
	switch agent {
	case "droid":
		return agentEvents{"PreToolUse", "PostToolUse", "Edit|Create|ApplyPatch", claudeShellMatcher}
	case "claude":
		return agentEvents{"PreToolUse", "PostToolUse", "Write|Edit|MultiEdit", claudeShellMatcher}
	case "codex":
		// Codex matchers are regular expressions on tool_name.
		return agentEvents{"PreToolUse", "PostToolUse", "^apply_patch$", "^Bash$"}
	case "copilot":
		// Copilot anchors its matchers to the whole tool name.
		return agentEvents{"preToolUse", "postToolUse", "apply_patch|create|edit|write|str_replace_editor", "bash|powershell"}
	case "devin":
		// Devin matchers are unanchored regular expressions on tool_name.
		return agentEvents{"PreToolUse", "PostToolUse", "^(edit|write|apply_patch|notebook_edit)$", "^exec$"}
	}
	return agentEvents{}
}

const claudeShellMatcher = "Bash|Shell|RunCommand|run_command|Execute|execute_command|Terminal"
