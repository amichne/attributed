#!/usr/bin/env bash
# Prepare the repository that docs/assets/blame.tape records. Run it from the
# checkout root. It builds git-attributed from this checkout into a temporary
# directory, commits a human-written function, records a Claude Code edit,
# and prints the path of a file for the recording shell to source.
set -euo pipefail

work="$(mktemp -d)"
mkdir -p "$work/bin" "$work/demo"
CGO_ENABLED=0 go build -trimpath -o "$work/bin/git-attributed" ./cmd/git-attributed
export PATH="$work/bin:$PATH"

cd "$work/demo"
git init -q -b main
git config user.name "Ada Lovelace"
git config user.email "ada@example.com"
git config core.hooksPath .git/hooks
git attributed install-hooks --agent claude --git --local-notes >/dev/null
printf 'def add(a, b):\n    return a + b\n' >math.py
git add math.py
git commit -qm "Add add" >/dev/null

# Claude Code adds mul(). Its PostToolUse hook records this checkpoint.
printf 'def add(a, b):\n    return a + b\n\ndef mul(a, b):\n    return a * b\n' >math.py
printf '%s' '{"session_id":"demo","tool_name":"Edit","tool_input":{"file_path":"math.py"},"model":"claude-opus-5-5"}' |
  git attributed checkpoint claude --type ai --hook-input stdin

cat >"$work/env.sh" <<EOF
export PATH="$work/bin:\$PATH"
export PS1='\[\e[1;32m\]❯\[\e[0m\] '
cd "$work/demo"
EOF
printf '%s\n' "$work/env.sh"
