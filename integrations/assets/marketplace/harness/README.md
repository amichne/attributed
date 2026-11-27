# Agent setup templates

This directory carries the two files each agent needs, ready to copy.

Install `git-attributed` with Go 1.24 or later and put `$(go env GOPATH)/bin`
on `PATH`. The setup commands find it there and stop if it is missing:

```sh
go install github.com/amichne/attributed/cmd/git-attributed@latest
```

## Agents with a package

Factory and Claude Code load [`../git-attributed`](../git-attributed), one
plugin directory with a manifest per agent and shared `commands/` and
`skills/`. Gemini CLI loads `commands/git-attributed-setup.toml` as an
extension. Each push to `main` publishes a small tree per agent to a
`harness/<agent>` branch:

| Agent | Install | Then run |
| --- | --- | --- |
| Claude Code | `claude plugin marketplace add 'https://github.com/amichne/attributed.git#harness/claude'` then `claude plugin install git-attributed@attributed` | `/git-attributed-setup` |
| Factory | Add `https://github.com/amichne/attributed` at branch `harness/factory` as a plugin marketplace, then install `git-attributed` | `/git-attributed-setup` |
| Gemini CLI | `gemini extensions install https://github.com/amichne/attributed --ref harness/gemini` | `/git-attributed-setup` |

Claude Code's validator accepts the `harness/claude` tree. The Factory and
Gemini install forms have not been run against their branches yet.

## Agents you set up by copying

Copy the setup command so the agent gains `/git-attributed-setup`, then run it.
The command finds the binary, installs the Git hooks, and copies the hook
file below. Copying the hook file by hand works too.

| Agent | Setup command | Copy it to | Hook file | Copy it to |
| --- | --- | --- | --- | --- |
| GitHub Copilot | [`copilot/git-attributed-setup.prompt.md`](copilot/git-attributed-setup.prompt.md) | `.github/prompts/` | [`copilot/hooks.json`](copilot/hooks.json) | `.github/hooks/git-attributed.json` |
| VS Code Agent | [`vscode/git-attributed-setup.prompt.md`](vscode/git-attributed-setup.prompt.md) | `.github/prompts/` | [`vscode/hooks.json`](vscode/hooks.json) | `.github/hooks/git-attributed-vscode.json` |
| Cursor | [`cursor/git-attributed-setup.md`](cursor/git-attributed-setup.md) | `.cursor/commands/` | [`cursor/hooks.json`](cursor/hooks.json) | `.cursor/hooks.json` |
| Codex | [`codex/git-attributed-setup.md`](codex/git-attributed-setup.md) | `~/.codex/prompts/` | [`codex/hooks.json`](codex/hooks.json) | `.codex/hooks.json` |
| Windsurf | [`windsurf/git-attributed-setup.md`](windsurf/git-attributed-setup.md) | `.windsurf/workflows/` | [`windsurf/hooks.json`](windsurf/hooks.json) | `.windsurf/hooks.json` |
| Grok | [`grok/git-attributed-setup.md`](grok/git-attributed-setup.md) | the prompt directory your build reads | [`grok/hooks.json`](grok/hooks.json) | `.grok/hooks/git-attributed.json` |
| OpenCode | [`opencode/git-attributed-setup.md`](opencode/git-attributed-setup.md) | `.opencode/commands/` | [`opencode/git-attributed.ts`](opencode/git-attributed.ts) | `.opencode/plugins/git-attributed.ts` |
| Gemini CLI | none | | [`gemini/settings.json`](gemini/settings.json) | merge into `.gemini/settings.json` |

`.gemini/settings.json` and `.claude/settings.json` hold unrelated settings
too, so merge the `hooks` block instead of replacing the file. Every other
hook file in this table is complete and can be copied as is.

OpenCode's plugin is documented for Linux, macOS, and WSL. Because OpenCode
loads the plugin as code, its setup command checks the downloaded plugin's
SHA-256 before it moves the file into `.opencode/plugins`.

## Why these files are safe to copy

Each hook file is projected from typed source in `integrations/`, and
`tools/projection/project.py check` fails when a committed file drifts from
the projection. The hook bodies contain no absolute path and find
`git-attributed` through `PATH`. Codex and Cursor hooks resolve the project
root with `git rev-parse --show-toplevel`. Gemini CLI and Grok hooks read it
from `GEMINI_PROJECT_DIR` and `GROK_WORKSPACE_ROOT`. Copilot, VS Code, and
Windsurf hooks run in the directory the agent starts them in. The OpenCode
plugin uses its plugin context to resolve the project root.

Each hook calls `git-attributed checkpoint portable-<agent> --hook-input stdin`,
which records a snapshot before and after an edit. When `git-attributed` is
not on `PATH`, every hook exits 0 without output, so the agent keeps working. OpenCode's generated plugin
also observes `apply_patch` and Bash calls, and waits for each checkpoint
before OpenCode continues. Without the agent hook, git-attributed still annotates
commits, but agent edits arrive as plain `human` lines instead of `ai` lines,
because nothing observed them.

## Git hooks

The Git side is the same for every agent and needs no template:

```sh
git attributed install-hooks --agent none --git --project
```

Add `--local-notes` to keep attribution notes off ordinary pushes.
