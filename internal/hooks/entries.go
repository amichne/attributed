package hooks

// entryFormat reads and writes one managed hook entry in an agent's event
// array. update and remove report whether value is a managed entry for spec
// and whether it is already current.
type entryFormat interface {
	update(value any, spec hookSpec) (updated any, managed, current bool)
	remove(value any, spec hookSpec) (remaining any, removed, current bool)
	entry(spec hookSpec) map[string]any
}

// groupedEntries is the Claude Code shape that Factory, Codex, and Devin
// share: {"matcher": ..., "hooks": [{"type": "command", "command": ...}]}.
type groupedEntries struct{}

func (groupedEntries) update(value any, spec hookSpec) (any, bool, bool) {
	return updateManagedHook(value, spec)
}

func (groupedEntries) remove(value any, spec hookSpec) (any, bool, bool) {
	return removeManagedHook(value, spec)
}

func (groupedEntries) entry(spec hookSpec) map[string]any {
	return managedEntry(spec)
}

// copilotTimeoutSeconds is Copilot's own default.
const copilotTimeoutSeconds = 30

// copilotEntries is GitHub Copilot's flat shape:
// {"type": "command", "bash": ..., "matcher": ..., "timeoutSec": ...}.
type copilotEntries struct{}

func (copilotEntries) managed(value any, spec hookSpec) (map[string]any, string, bool) {
	entry, ok := value.(map[string]any)
	if !ok {
		return nil, "", false
	}
	matcher, _ := entry["matcher"].(string)
	command, _ := entry["bash"].(string)
	if matcher != spec.matcher || entry["type"] != "command" || !managedAgentCommand(command, spec) {
		return nil, "", false
	}
	return entry, command, true
}

func (format copilotEntries) update(value any, spec hookSpec) (any, bool, bool) {
	entry, command, ok := format.managed(value, spec)
	if !ok {
		return value, false, false
	}
	if command == spec.command {
		return value, true, true
	}
	updated := make(map[string]any, len(entry))
	for key, field := range entry {
		updated[key] = field
	}
	updated["bash"] = spec.command
	return updated, true, false
}

func (format copilotEntries) remove(value any, spec hookSpec) (any, bool, bool) {
	_, command, ok := format.managed(value, spec)
	if !ok {
		return value, false, false
	}
	return nil, true, command == spec.command
}

func (copilotEntries) entry(spec hookSpec) map[string]any {
	return map[string]any{
		"type":       "command",
		"bash":       spec.command,
		"matcher":    spec.matcher,
		"timeoutSec": copilotTimeoutSeconds,
	}
}

// ownedConfigEmpty reports whether an owned hook file holds nothing but its
// format version.
func ownedConfigEmpty(config map[string]any) bool {
	for key := range config {
		if key != "version" {
			return false
		}
	}
	return true
}
