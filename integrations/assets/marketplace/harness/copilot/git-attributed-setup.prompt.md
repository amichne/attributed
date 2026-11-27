---
mode: agent
description: Install git-attributed and activate attribution hooks
---

# git-attributed setup

Activate git-attributed attribution hooks for the current project. Never use
`sudo`.

**1. Find the binary.**

Find `git-attributed` on `PATH`.

Linux or macOS:

```sh
binary="$(command -v git-attributed)" || { echo "git-attributed is not on PATH" >&2; exit 1; }
```

Windows PowerShell:

```powershell
$binary = (Get-Command git-attributed -ErrorAction Stop).Source
```

If it is missing, stop. Tell the user to install it with
`brew install amichne/tap/git-attributed`, or with Go 1.24 or later through
`go install github.com/amichne/attributed/cmd/git-attributed@latest` with
`$(go env GOPATH)/bin` on `PATH`, then rerun this command. Run the remaining commands through the absolute
`$binary` path, so setup does not depend on this process reloading `PATH`.
Verify `$binary version`.

**2. Install the Copilot and Git hooks.**

```sh
"$binary" install-hooks --agent copilot --git --project
```

This writes `.github/hooks/git-attributed.json` and installs `post-commit`
attribution and `pre-push` note sharing for the current repository.
Attribution notes contain repository paths, line ranges, agent and model
names, human identity tokens, session identifiers, timestamps, and blob IDs,
and ordinary pushes publish them to the same remote. Tell the user this.
Rerun with `--local-notes` if the user wants attribution without automatic
note sharing.

**3. Verify and report.**

Run `"$binary" status`. Copilot CLI loads repository hooks only from a
trusted folder. Tell the user to restart Copilot and trust the folder if
asked. Stop and report the exact error when hook installation or status
verification fails.
