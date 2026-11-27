package hooks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectAgents(t *testing.T) {
	t.Parallel()
	for value, want := range map[string][]string{
		"all":                  {"claude", "droid", "codex", "copilot", "devin"},
		"none":                 nil,
		"codex":                {"codex"},
		"copilot, devin,codex": {"copilot", "devin", "codex"},
		"claude,claude":        {"claude"},
	} {
		got, err := SelectAgents(value)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("SelectAgents(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "cursor", "claude,", "all,claude"} {
		if _, err := SelectAgents(value); err == nil {
			t.Errorf("SelectAgents(%q) accepted", value)
		}
	}
}

// agentHooks returns the hook commands an installed agent file runs for event,
// keyed by matcher.
func agentHooks(t *testing.T, config map[string]any, nested, copilot bool, event string) map[string]string {
	t.Helper()
	events := config
	if nested {
		events, _ = config["hooks"].(map[string]any)
	}
	entries, _ := events[event].([]any)
	commands := map[string]string{}
	for _, value := range entries {
		entry, _ := value.(map[string]any)
		matcher, _ := entry["matcher"].(string)
		if copilot {
			commands[matcher], _ = entry["bash"].(string)
			continue
		}
		hooks, _ := entry["hooks"].([]any)
		for _, hook := range hooks {
			command, _ := hook.(map[string]any)["command"].(string)
			commands[matcher] = command
		}
	}
	return commands
}

func TestInstallWritesEachAgentsNativeFormat(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		agent, path      string
		nested, copilot  bool
		pre, post        string
		edit, shell      string
		wantVersionField bool
	}{
		{"codex", ".codex/hooks.json", true, false, "PreToolUse", "PostToolUse", "^apply_patch$", "^Bash$", false},
		{"copilot", ".github/hooks/git-attributed.json", true, true, "preToolUse", "postToolUse",
			"apply_patch|create|edit|write|str_replace_editor", "bash|powershell", true},
		{"devin", ".devin/hooks.v1.json", false, false, "PreToolUse", "PostToolUse",
			"^(edit|write|apply_patch|notebook_edit)$", "^exec$", false},
	} {
		t.Run(test.agent, func(t *testing.T) {
			t.Parallel()
			root := hookRepo(t)
			result, err := Install(root, Options{Agent: test.agent})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.FromSlash(test.path))
			if !reflect.DeepEqual(result.Changed, []string{path}) {
				t.Fatalf("Install changed = %v, want %s", result.Changed, path)
			}
			config := readObject(t, path)
			if _, ok := config["version"]; ok != test.wantVersionField {
				t.Fatalf("version field present = %t, want %t", ok, test.wantVersionField)
			}
			for event, kind := range map[string]string{test.pre: "human", test.post: "ai"} {
				commands := agentHooks(t, config, test.nested, test.copilot, event)
				want := "'git-attributed' checkpoint " + test.agent + " --managed-by git-attributed --type " + kind + " --hook-input stdin"
				for _, matcher := range []string{test.edit, test.shell} {
					if !strings.HasPrefix(commands[matcher], guardProbe) || !strings.HasSuffix(commands[matcher], want) {
						t.Fatalf("%s %q runs %q, want a guarded %q", event, matcher, commands[matcher], want)
					}
				}
				if len(commands) != 2 {
					t.Fatalf("%s has hooks %v, want only the edit and shell hooks", event, commands)
				}
			}
			again, err := Install(root, Options{Agent: test.agent})
			if err != nil || len(again.Changed) != 0 {
				t.Fatalf("second Install = %+v, %v; want no change", again, err)
			}
		})
	}
}

func TestInstallKeepsOtherHooksInAgentFiles(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	write := func(path, content string) {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".codex/hooks.json", `{"description":"team","hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":"./policy.sh"}]}]}}`)
	write(".devin/hooks.v1.json", `{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./validate.sh","timeout":10}]}]}`)
	if _, err := Install(root, Options{Agent: "codex,devin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root, Options{Agent: "codex,devin"}); err != nil {
		t.Fatal(err)
	}
	codex := readObject(t, filepath.Join(root, ".codex", "hooks.json"))
	if codex["description"] != "team" || agentHooks(t, codex, true, false, "PreToolUse")["^Bash$"] != "./policy.sh" {
		t.Fatalf("codex hooks after uninstall = %v", codex)
	}
	if _, ok := codex["hooks"].(map[string]any)["PostToolUse"]; ok {
		t.Fatalf("codex PostToolUse kept after uninstall: %v", codex)
	}
	devin := readObject(t, filepath.Join(root, ".devin", "hooks.v1.json"))
	if agentHooks(t, devin, false, false, "PreToolUse")["exec"] != "./validate.sh" || len(devin) != 1 {
		t.Fatalf("devin hooks after uninstall = %v", devin)
	}
}

func TestUninstallRemovesTheCopilotHookFile(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	if _, err := Install(root, Options{Agent: "copilot"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "hooks", "git-attributed.json")
	result, err := Uninstall(root, Options{Agent: "copilot"})
	if err != nil || !reflect.DeepEqual(result.Changed, []string{path}) {
		t.Fatalf("Uninstall = %+v, %v", result, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists: %v", path, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 0 {
		t.Fatalf("left behind %v, %v", entries, err)
	}
}

func TestInstallRejectsAnotherCopilotHookVersion(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".github", "hooks", "git-attributed.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "copilot"}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Install over version 2 = %v", err)
	}
}

func TestUserScopeAgentFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("COPILOT_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	for agent, want := range map[string]string{
		"codex":   filepath.Join(home, ".codex", "hooks.json"),
		"copilot": filepath.Join(home, ".copilot", "hooks", "git-attributed.json"),
		"devin":   filepath.Join(home, ".config", "devin", "config.json"),
	} {
		file, err := agentConfigFile("", agent, true)
		if err != nil || file.path != want {
			t.Errorf("user %s file = %q, %v; want %q", agent, file.path, err, want)
		}
	}
	codexHome := filepath.Join(home, "codex-home")
	copilotHome := filepath.Join(home, "copilot-home")
	xdg := filepath.Join(home, "xdg")
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("COPILOT_HOME", copilotHome)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	for agent, want := range map[string]string{
		"codex":   filepath.Join(codexHome, "hooks.json"),
		"copilot": filepath.Join(copilotHome, "hooks", "git-attributed.json"),
		"devin":   filepath.Join(xdg, "devin", "config.json"),
	} {
		file, err := agentConfigFile("", agent, true)
		if err != nil || file.path != want {
			t.Errorf("user %s file with override = %q, %v; want %q", agent, file.path, err, want)
		}
	}
	if file, err := agentConfigFile("", "devin", true); err != nil || !file.nested {
		t.Errorf("user devin config nests hooks = %t, %v", file.nested, err)
	}
}
