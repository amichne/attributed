package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// legacyVSCodeHookFile is the VS Code hook file the setup templates used to
// copy into .github/hooks/. VS Code now runs the same hooks as Copilot CLI,
// which also loads every .github/hooks/*.json. Copilot ran this file's
// PascalCase hooks too, and its portable-vscode checkpoint failed on
// Copilot's patch input, which made Copilot deny the edit.
const legacyVSCodeHookFile = "git-attributed-vscode.json"

func legacyVSCodePath(root string) string {
	return filepath.Join(root, ".github", "hooks", legacyVSCodeHookFile)
}

// isLegacyVSCodeCommand reports whether a hook command is one the projected
// VS Code hook file carried.
func isLegacyVSCodeCommand(command string) bool {
	return strings.Contains(command, " checkpoint portable-vscode ") &&
		strings.Contains(command, "# git-attributed-hook:vscode-")
}

// stripLegacyVSCodeHooks removes git-attributed's entries from a legacy VS
// Code hook file and reports whether it removed any. Other entries stay.
func stripLegacyVSCodeHooks(config map[string]any) (bool, error) {
	events, ok := config["hooks"].(map[string]any)
	if !ok {
		return false, nil
	}
	changed := false
	for event, value := range events {
		entries, err := objectArray(value)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", event, err)
		}
		kept := make([]any, 0, len(entries))
		for _, entry := range entries {
			fields, ok := entry.(map[string]any)
			command, _ := fields["command"].(string)
			if ok && isLegacyVSCodeCommand(command) {
				changed = true
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(events, event)
		} else {
			events[event] = kept
		}
	}
	if len(events) == 0 {
		delete(config, "hooks")
	}
	return changed, nil
}

// migrateLegacyVSCodeHooks removes git-attributed's entries from a project's
// legacy VS Code hook file, deleting the file when nothing else is left.
func migrateLegacyVSCodeHooks(root string) (bool, error) {
	path := legacyVSCodePath(root)
	if err := rejectSymlinkPath(root, filepath.Dir(path)); err != nil {
		return false, err
	}
	config, mode, existed, err := readJSONObject(path)
	if err != nil || !existed {
		return false, err
	}
	changed, err := stripLegacyVSCodeHooks(config)
	if err != nil {
		return false, fmt.Errorf("update hooks in %s: %w", path, err)
	}
	if !changed {
		return false, nil
	}
	if len(config) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove %s: %w", path, err)
		}
		return true, nil
	}
	if err := createBackup(path, mode); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", path, err)
	}
	if err := atomicWrite(path, append(data, '\n'), mode); err != nil {
		return false, err
	}
	return true, nil
}

// LegacyVSCodeHooks returns the path of a legacy VS Code hook file in the
// repository at root that still holds git-attributed entries, or "".
func LegacyVSCodeHooks(root string) (string, error) {
	path := legacyVSCodePath(root)
	config, _, existed, err := readJSONObject(path)
	if err != nil || !existed {
		return "", err
	}
	found, err := stripLegacyVSCodeHooks(config)
	if err != nil || !found {
		return "", err
	}
	return path, nil
}
