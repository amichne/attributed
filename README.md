# Attributed

Which lines in a commit can you trace to an observed agent edit? Attributed aims to answer that question with a small Go distribution derived from Comarch’s git-byline. Agent hooks capture edits, a reconciler matches the evidence to the lines Git actually commits, and Git notes carry the result alongside history.

**Extraction is in progress.** This tree imports the pinned upstream source with every `byline` name changed to `attributed`. It builds `git-attributed`, which Git runs as `git attributed`. Notes live under `refs/notes/attributed`. Dashboards, statistics, policy checks, disclosure, forge workflows, self-update, and token accounting have been removed. No release exists yet, so build the binary from this checkout. `git-attributed import --format byline` brings existing git-byline notes across. Like upstream, it can label unexplained lines as human, and overlapping shell events can be ambiguous when the harness supplies no event ID.

## Start here

- [Reproduce the upstream baseline](docs/baseline.mdx) to build the pinned binary, run its tests, and compare sizes under the same conditions.
- [Read the qualification plan](docs/qualification.mdx) for the package boundaries, acceptance checks, and decisions that need separate proposals.
- [Follow the extraction slices](docs/extraction.mdx) for the import, the naming map, and the order of later changes.

Build and test from this checkout with Go 1.24 or later:

```sh
CGO_ENABLED=0 go build -trimpath -o git-attributed ./cmd/git-attributed
go test ./...
ATTRIBUTED_BIN=./git-attributed bash scripts/smoke.sh
```

`marketplace/` and `commands/` are generated from `integrations/`. With Pkl 0.33.0 and `uv` installed, check them with `uv run --locked --script tools/projection/project.py check`. The [projection section](docs/extraction.mdx#project-harness-integrations) explains how to change and regenerate them.

## Design constraints

Keep Go, the stock Git executable, and Git’s object and ref storage. Developers should be able to read a commit’s attribution after uninstalling the producer. The design excludes daemons, hosted services, external databases, Git replacements, and new runtime frameworks.

Carry an attribution only as far as the evidence supports it. Unobserved authorship stays unknown. Preserve ambiguity, including when two sessions could have produced the same edit. Published records must exclude prompts and transcripts. Keep compatible records readable wherever their meaning can be preserved.

## Documentation

With Node.js and the Mintlify CLI installed, preview the documentation from this checkout:

```sh
cd docs
mint dev --no-open
```

Run `mint validate` and `mint broken-links` in the same directory before committing documentation changes. Hosted publication requires separate authorization.

## License

Attributed is available under the [MIT License](LICENSE). It derives from git-byline, which Comarch S.A. licenses under MIT, so copies keep the [upstream notice](LICENSE.upstream) as well. Every binary embeds both notices, and `git-attributed license` prints them. [Pinned upstream license](https://github.com/comarch/git-byline/blob/d4260838249720bcea3a746cce809e5a1955248a/LICENSE).
