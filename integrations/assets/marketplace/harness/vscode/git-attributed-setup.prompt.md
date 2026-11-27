---
mode: agent
description: Install git-attributed and activate attribution hooks
---

# git-attributed setup

Activate git-attributed attribution hooks for the current project. Never use
`sudo`.

**1. Find the binary.**

Attributed has no release yet, so this command does not download a binary.
Find `git-attributed` on `PATH`.

Linux or macOS:

```sh
binary="$(command -v git-attributed)" || { echo "git-attributed is not on PATH" >&2; exit 1; }
```

Windows PowerShell:

```powershell
$binary = (Get-Command git-attributed -ErrorAction Stop).Source
```

If it is missing, stop. Tell the user to build it from an Attributed checkout
with Go 1.24 or later, as in
`go build -trimpath -o <directory on PATH>/git-attributed ./cmd/git-attributed`,
and then rerun this command. Run the remaining commands through the absolute
`$binary` path, so setup does not depend on this process reloading `PATH`.
Verify `$binary version`.

**2. Install the Git hooks.**

```sh
"$binary" install-hooks --agent none --git --project
```

This installs `post-commit` attribution and `pre-push` note sharing for the
current repository. Attribution notes contain repository paths, line ranges,
agent and model names, human identity tokens, session identifiers,
timestamps, and blob IDs, and ordinary pushes publish them to the same
remote. Tell the user this. Rerun with `--local-notes` if the user wants
attribution without automatic note sharing.

**3. Install the agent hook for this agent.**

Copy `marketplace/harness/vscode/hooks.json` from the git-attributed repository into
`.github/hooks/git-attributed-vscode.json` of the user's project, creating parent directories as needed:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  -o .github/hooks/git-attributed-vscode.json --create-dirs \
  https://raw.githubusercontent.com/amichne/attributed/main/marketplace/harness/vscode/hooks.json
```

The same file is checked in beside this command, so it can also be copied
from a local clone. It calls `git-attributed checkpoint portable-vscode
--hook-input stdin` and resolves `git-attributed` through `PATH`. Do not edit the
hook bodies.

**4. Verify and report.**

Run `"$binary" status`. Tell the user to add the reported install directory
to `PATH` and to restart this agent so the hook is picked up. Stop and report
the exact error when download, checksum, version, hook installation, or
status verification fails.
