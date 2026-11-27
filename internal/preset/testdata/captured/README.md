# Captured hook payloads

Hook input recorded from real agent sessions on 2026-11-27, with the session
directory replaced by `/workspace/repo`. Field names, value types, and
identifiers are as the agents sent them.

| File prefix | Agent | Hook file |
| --- | --- | --- |
| `codex-` | Codex CLI 0.172.0 | `.codex/hooks.json` |
| `copilot-` | GitHub Copilot CLI 1.0.116 | `.github/hooks/*.json`, camelCase events |
| `copilot-claude-format-` | GitHub Copilot CLI 1.0.116 | `.claude/settings.json`, which Copilot also runs |
| `claude-` | Claude Code 2.1.332 | `.claude/settings.json` |

Devin CLI was not available, so its tests use payloads built from its
documentation instead of captures.
