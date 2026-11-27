# Attributed

**See which lines in every commit an AI agent wrote, stored in plain Git notes beside your history.**

[![CI](https://github.com/amichne/attributed/actions/workflows/ci.yml/badge.svg)](https://github.com/amichne/attributed/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-266452)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/amichne/attributed)](go.mod)
[![Docs](https://img.shields.io/badge/docs-attributed.mintlify.app-266452)](https://attributed.mintlify.app)

![git attributed blame shows Ada's lines as human and Claude Code's lines as ai:claude/claude-opus-5-5](docs/assets/blame.gif)

Agent hooks snapshot each file before and after the agent edits it. When you commit, a Git hook matches those snapshots to what Git committed and writes the line ranges to `refs/notes/attributed`.

## Install

On macOS or Linux:

```bash
brew install amichne/tap/git-attributed
```

Or build it with Go 1.24 or later and put `$(go env GOPATH)/bin` on `PATH`:

```bash
go install github.com/amichne/attributed/cmd/git-attributed@latest
```

Git runs the binary as `git attributed`. Release archives and their checksums are on the [releases page](https://github.com/amichne/attributed/releases), and the docs explain [installing through a Homebrew mirror](https://attributed.mintlify.app/#install-through-a-homebrew-mirror).

## Quick start

```bash
git attributed install-hooks --agent claude --git
```

Work with Claude Code as usual and commit. Then:

```bash
git attributed blame path/to/file
```

Use `--agent droid` for Factory. For other agents, see [`marketplace/harness`](marketplace/harness/README.md). Add `--local-notes` to keep notes out of your pushes.

## What you can rely on

- **Your commits always land.** Annotation runs after the commit and never blocks it.
- **Missing tooling is quiet.** On a machine without `git-attributed`, every hook skips silently. Annotation resumes when the binary returns.
- **Notes are never overwritten.** A push with conflicting notes stops instead of replacing anything.
- **It is just Git.** Everything lives in refs, notes, and hooks you can read or delete with plain Git, even after uninstalling.

Unobserved lines count as the commit author's, and the docs list the other rough edges. [What you can rely on](https://attributed.mintlify.app/safety) and [How Attributed uses Git](https://attributed.mintlify.app/git) cover the details.

## Development

```bash
go test ./...
go build -o git-attributed ./cmd/git-attributed && ATTRIBUTED_BIN=./git-attributed bash scripts/smoke.sh
```

Pull request titles follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`). [release-please](https://github.com/googleapis/release-please) reads them on `main` to keep a release pull request with the next version and `CHANGELOG.md`; merging it publishes the release, and GoReleaser builds the archives and updates the Homebrew cask. `bash scripts/cask-smoke.sh` installs that cask from a local mirror.

`marketplace/` and `commands/` are generated from `integrations/`. Edit the source, then regenerate as the [projection guide](docs/extraction.mdx#project-harness-integrations) describes. `vhs docs/assets/blame.tape` re-records the demo.

### Docs publishing

Mintlify publishes `docs/` from `main` to
[attributed.michne.com](https://attributed.michne.com) through its GitHub
integration. GitHub CI validates the Mintlify build and internal links. Check the commit in
[Mintlify Publishes](https://dashboard.mintlify.com/attributed) and confirm the
changed pages on the live site. If a docs commit never starts a build, check
the Mintlify GitHub App's access to `amichne/attributed` in Git Settings.

## License

[MIT](LICENSE). Attributed derives from [git-byline](https://github.com/comarch/git-byline) by Comarch S.A., also MIT, so copies keep the [upstream notice](LICENSE.upstream). `git attributed license` prints both notices.
