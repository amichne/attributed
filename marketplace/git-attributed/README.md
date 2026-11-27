# git-attributed plugin

Record which committed lines an AI agent wrote, in Git notes beside your
history. Nothing leaves your machine except the notes ref on your own pushes.

This plugin adds `/git-attributed-setup` and a `git-attributed` skill to
Claude Code and Factory.

## Quick start

Install the binary:

```sh
brew install amichne/tap/git-attributed
```

Or build it with Go 1.24 or later and put `$(go env GOPATH)/bin` on `PATH`:

```sh
go install github.com/amichne/attributed/cmd/git-attributed@latest
```

Then add the marketplace and the plugin. For Claude Code:

```sh
claude plugin marketplace add 'https://github.com/amichne/attributed.git#harness/claude'
claude plugin install git-attributed@attributed
```

Start the agent in a Git repository and run `/git-attributed-setup`. After
your next commit:

```sh
git attributed blame path/to/file
git attributed status
```

## What setup does

- Finds `git-attributed` on `PATH` and stops if it is missing.
- Adds agent and Git hooks without replacing your own hook code.
- Shares `refs/notes/attributed` on ordinary pushes, unless you choose
  `--local-notes`.

Hooks skip silently on machines without the binary. See the
[documentation](https://attributed.michne.com) for what Attributed writes
to Git and what it guarantees.
