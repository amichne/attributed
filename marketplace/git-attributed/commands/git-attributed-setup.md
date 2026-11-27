---
description: Activate git-attributed attribution hooks
---

Activate git-attributed attribution hooks for the current project. Attributed
has no release yet, so this command does not download a binary. Find
`git-attributed` on `PATH`.

On Linux or macOS, run:

```sh
binary="$(command -v git-attributed)" || { echo "git-attributed is not on PATH" >&2; exit 1; }
```

On Windows PowerShell, run:

```powershell
$binary = (Get-Command git-attributed -ErrorAction Stop).Source
```

If it is missing, stop. Tell the user to install it with
`brew install amichne/tap/git-attributed`, or with Go 1.24 or later through
`go install github.com/amichne/attributed/cmd/git-attributed@latest` with
`$(go env GOPATH)/bin` on `PATH`, then rerun this command. Run the remaining
commands through the absolute
`$binary` path so setup does not depend on the current agent process reloading
`PATH`. Verify `$binary version`.

This plugin is installed by both Factory and Claude Code, so pick the agent
you are running:

- Factory: `--agent droid`, and its project hook file is `.factory/hooks.json`
- Claude Code: `--agent claude`, and its project hook file is
  `.claude/settings.json`

If that project hook file already contains `git-attributed-hook`, keep it
and install only the Git hooks:

```sh
"$binary" install-hooks --agent none --git --project
```

Otherwise install the user-level agent hook for the agent you are running,
plus the current repository Git hooks. For Factory:

```sh
"$binary" install-hooks --agent droid --git --user
```

For Claude Code use `--agent claude` in the same command.

Git hook installation shares `refs/notes/attributed` with the selected remote on
ordinary pushes. Tell the user that notes contain paths, line ranges, agent
and model names, human identity tokens, session identifiers, timestamps, and
blob IDs. If the user wants automatic note sharing disabled, rerun the
selected hook command with `--local-notes`. Notes can still be pushed
explicitly.

Run `$binary status`. Tell the user to restart active agents before relying
on the hooks. Stop and report the exact error when hook installation or
status verification fails. Never use `sudo`.
