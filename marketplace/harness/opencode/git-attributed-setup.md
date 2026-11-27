# git-attributed setup

Activate git-attributed attribution hooks for the current project. Never use
`sudo`.

On Windows, run OpenCode inside WSL and follow the Linux or macOS commands in
the WSL shell. The plugin is declared Unix-only, and native Windows plugin
loading has not been verified. If the user runs OpenCode natively on Windows,
use the PowerShell commands below and tell the user that this setup is
unverified.

**1. Find the binary.**

Find `git-attributed` on `PATH`.

Linux, macOS, or WSL:

```sh
binary="$(command -v git-attributed)" || { echo "git-attributed is not on PATH" >&2; exit 1; }
```

Native Windows PowerShell:

```powershell
$binary = (Get-Command git-attributed -ErrorAction Stop).Source
```

If it is missing, stop. Tell the user to install it with Go 1.24 or later,
`go install github.com/amichne/attributed/cmd/git-attributed@latest`, put
`$(go env GOPATH)/bin` on `PATH`, and rerun this command. Run the remaining commands through the absolute
`$binary` path, so setup does not depend on this process reloading `PATH`.
PowerShell needs the call operator for that, as in `& $binary version`.
Verify `$binary version`.

**2. Install the Git hooks.**

```sh
"$binary" install-hooks --agent none --git --project
```

In PowerShell, run `& $binary install-hooks --agent none --git --project`.

This installs `post-commit` attribution and `pre-push` note sharing for the
current repository. Attribution notes contain repository paths, line ranges,
agent and model names, human identity tokens, session identifiers,
timestamps, and blob IDs, and ordinary pushes publish them to the same
remote. Tell the user this. Rerun with `--local-notes` if the user wants
attribution without automatic note sharing.

**3. Install the OpenCode plugin.**

Install `marketplace/harness/opencode/git-attributed.ts` from the
git-attributed repository into `.opencode/plugins/git-attributed.ts` of the
user's project. OpenCode loads everything in that directory as code. The
commands therefore download the plugin to a temporary file, compare its
SHA-256 with the value below, and move it into place only when the two match.
Each command stops at its first failed step and deletes the temporary file.
Do not change the SHA-256. On a mismatch, stop and report both values.

Linux, macOS, or WSL:

```sh
(
  set -eu
  sha256=c483c737589f19f259dbf35f6e9f85092cdd8c0b20916d45f90df689deb014c5
  plugin="$(mktemp)"
  trap 'rm -f "$plugin"' EXIT
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 -o "$plugin" \
    "https://raw.githubusercontent.com/amichne/attributed/main/marketplace/harness/opencode/git-attributed.ts"
  if command -v sha256sum >/dev/null 2>&1; then
    digest_line="$(sha256sum "$plugin")"
  else
    digest_line="$(shasum -a 256 "$plugin")"
  fi
  actual="${digest_line%% *}"
  if [ "$actual" != "$sha256" ]; then
    echo "plugin checksum mismatch: want $sha256, got $actual" >&2
    exit 1
  fi
  mkdir -p .opencode/plugins
  chmod 644 "$plugin"
  mv "$plugin" .opencode/plugins/git-attributed.ts
)
```

Native Windows PowerShell, where `curl` is an alias for `Invoke-WebRequest`
and rejects the flags above:

```powershell
$sha256 = "c483c737589f19f259dbf35f6e9f85092cdd8c0b20916d45f90df689deb014c5"
$plugin = Join-Path ([IO.Path]::GetTempPath()) ("git-attributed-" + [guid]::NewGuid() + ".ts")
try {
    irm "https://raw.githubusercontent.com/amichne/attributed/main/marketplace/harness/opencode/git-attributed.ts" -OutFile $plugin -ErrorAction Stop
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $plugin -ErrorAction Stop).Hash
    if ($actual -ne $sha256) {
        throw "plugin checksum mismatch: want $sha256, got $actual"
    }
    New-Item -ItemType Directory -Force -Path .opencode/plugins -ErrorAction Stop | Out-Null
    Move-Item -Force -LiteralPath $plugin .opencode/plugins/git-attributed.ts -ErrorAction Stop
}
catch {
    # Rethrow, so powershell -File exits non-zero when the download fails.
    throw
}
finally {
    # After a good move a failed Remove-Item would make the block exit non-zero.
    if (Test-Path -LiteralPath $plugin) { Remove-Item -LiteralPath $plugin -Force -ErrorAction SilentlyContinue }
}
```

This plugin captures OpenCode edit, write, apply_patch, and Bash tool
events, and waits for each checkpoint before OpenCode continues. OpenCode
does not include the active model in these events, so the checkpoint model
stays `unknown`. MCP calls, some subagent paths, and failed tool calls do not
have a dedicated plugin event. Do not edit the installed plugin.

**4. Verify and report.**

Run `"$binary" status`, or `& $binary status` in PowerShell. Tell the user to
restart OpenCode so it loads the plugin. Stop and report the exact error when
the plugin download, its checksum, hook installation, or status verification
fails.
