# Attributed

Which lines in a commit can you trace to an observed agent edit? Attributed aims to answer that question with a small Go distribution derived from Comarch’s git-byline. Agent hooks capture edits, a reconciler matches the evidence to the lines Git actually commits, and Git notes carry the result alongside history.

**Upstream qualification is in progress.** This first commit pins the reference implementation and records its build, tests, and known gaps. The Attributed CLI is still to be extracted. Upstream can label unexplained lines as human, and overlapping shell events can be ambiguous when the harness supplies no event ID.

## Start here

- [Reproduce the upstream baseline](docs/baseline.mdx) to build the pinned binary, run its tests, and compare sizes under the same conditions.
- [Read the qualification plan](docs/qualification.mdx) for the package boundaries, acceptance checks, and decisions that need separate proposals.

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

## Upstream license

Comarch S.A. licenses git-byline under MIT. Include the [upstream copyright and permission notice](LICENSE.upstream) with copies or substantial portions of the upstream software, including binary distributions. [Pinned source](https://github.com/comarch/git-byline/blob/d4260838249720bcea3a746cce809e5a1955248a/LICENSE).
