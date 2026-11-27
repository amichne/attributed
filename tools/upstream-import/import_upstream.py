#!/usr/bin/env -S uv run --locked --script
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
"""Reproduce Attributed's import of the pinned git-byline tree.

The import reads blobs from the pinned commit, not from a worktree. It drops
upstream repository automation and agent configuration, renames every
byline identifier to its attributed counterpart, and refuses any residual
upstream name outside the files whose content must stay verbatim.

    uv run --locked --script tools/upstream-import/import_upstream.py SOURCE DEST

SOURCE is a git-byline clone that contains the pinned commit. DEST must not
exist. Compare DEST with the import commit to verify it.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING, Final, Literal

if TYPE_CHECKING:
    from collections.abc import Callable, Sequence

PIN: Final = "d4260838249720bcea3a746cce809e5a1955248a"

# Paths that configure upstream's own repository, agents, hosting, release
# process, or documentation site. Each entry names a file or a directory.
EXCLUDED: Final[tuple[str, ...]] = (
    # Agent instructions and hooks for contributors to upstream.
    ".claude",
    ".codex",
    ".cursor",
    ".factory",
    ".gemini",
    ".grok",
    ".opencode",
    ".windsurf",
    ".promptscript",
    "promptscript.yaml",
    "AGENTS.md",
    "CLAUDE.md",
    "GEMINI.md",
    "OPENCODE.md",
    "gemini-extension.json",
    # Upstream marketplace listings and hosted automation.
    ".claude-plugin",
    ".factory-plugin",
    ".github",
    ".gitlab",
    ".coderabbit.yaml",
    ".goreleaser.yml",
    ".release-please-manifest.json",
    "release-please-config.json",
    "renovate.json",
    "codecov.yml",
    "sonar-project.properties",
    "tools/validate",
    # Upstream project documents. Attributed keeps its own.
    "docs",
    "website",
    "README.md",
    "CHANGELOG.md",
    "CODE_OF_CONDUCT.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    # Kept at the Attributed root as LICENSE.upstream.
    "LICENSE",
)

# Files whose content carries a copyright notice and stays byte-identical.
VERBATIM: Final[frozenset[str]] = frozenset({"marketplace/git-byline/LICENSE"})

# Ordered rules. Specific forms precede the general case rules.
RULES: Final[tuple[tuple[str, str], ...]] = (
    ('"name": "Wojciech Guziak (Comarch S.A.)"', '"name": "Austin Michne"'),
    ("author: Comarch\n", "author: Austin Michne\n"),
    ("comarch.git-byline", "amichne.attributed"),
    ("comarch/git-byline", "amichne/attributed"),
    ("git-byline", "git-attributed"),
    ("BYLINE", "ATTRIBUTED"),
    ("Byline", "Attributed"),
    ("byline", "attributed"),
)

# Fixture bytes derived from the producer name. Git AI export hashes the
# synthetic human author and the checkpoint trace seed, both of which name
# the producer: sha256("git-attributed") and
# sha256("git-attributed:checkpoint:7"), truncated to 14 hex digits.
DERIVED: Final[dict[str, tuple[tuple[str, str], ...]]] = {
    "internal/interop/testdata/gitai-export.golden": (
        ("h_6e3b111ed570c7", "h_d550ff2fbe4333"),
        ("t_661df9ba1b7b4e", "t_26f7b413a2fd2f"),
    ),
}

RESIDUAL: Final = re.compile(r"byline|comarch", re.IGNORECASE)

type Mode = Literal["100644", "100755"]


class ImportFailure(Exception):
    """The pinned tree cannot be imported under the declared rules."""


@dataclass(frozen=True)
class Entry:
    mode: Mode
    oid: str
    path: str


@dataclass(frozen=True)
class Output:
    mode: Mode
    path: str
    content: bytes


def excluded(path: str) -> bool:
    return any(path == item or path.startswith(item + "/") for item in EXCLUDED)


def rename(text: str, rules: Sequence[tuple[str, str]] = RULES) -> str:
    for old, new in rules:
        text = text.replace(old, new)
    return text


def transform(entry: Entry, content: bytes) -> Output:
    path = rename(entry.path)
    if RESIDUAL.search(path):
        raise ImportFailure(f"{entry.path}: renamed path keeps an upstream name: {path}")
    if entry.path in VERBATIM:
        return Output(entry.mode, path, content)
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as error:
        raise ImportFailure(
            f"{entry.path}: not UTF-8 text; add an exclusion or a verbatim rule"
        ) from error
    renamed = rename(text)
    for old, _ in DERIVED.get(entry.path, ()):
        if old not in renamed:
            raise ImportFailure(f"{entry.path}: derived value {old} is absent")
    renamed = rename(renamed, DERIVED.get(entry.path, ()))
    if found := RESIDUAL.search(renamed):
        line = renamed.count("\n", 0, found.start()) + 1
        raise ImportFailure(f"{entry.path}:{line}: renamed content keeps {found.group()!r}")
    return Output(entry.mode, path, renamed.encode("utf-8"))


def plan(entries: Sequence[Entry], read: Callable[[str], bytes]) -> list[Output]:
    outputs = [transform(entry, read(entry.oid)) for entry in entries if not excluded(entry.path)]
    paths = [output.path for output in outputs]
    if len(paths) != len(set(paths)):
        raise ImportFailure("renaming maps two upstream paths to one destination")
    return outputs


def parse_tree(listing: bytes) -> list[Entry]:
    entries: list[Entry] = []
    for record in listing.split(b"\0"):
        if not record:
            continue
        meta, _, raw_path = record.partition(b"\t")
        mode, kind, oid = meta.decode("ascii").split(" ")
        path = raw_path.decode("utf-8")
        if kind != "blob" or mode not in ("100644", "100755"):
            raise ImportFailure(f"{path}: unsupported tree entry {mode} {kind}")
        entries.append(Entry("100755" if mode == "100755" else "100644", oid, path))
    return entries


def _git(source: Path, *args: str) -> bytes:
    result = subprocess.run(["git", "-C", str(source), *args], capture_output=True, check=False)
    if result.returncode != 0:
        detail = result.stderr.decode("utf-8", "replace").strip()
        raise ImportFailure(f"git {args[0]} failed: {detail}")
    return result.stdout


def write(dest: Path, outputs: Sequence[Output]) -> None:
    if dest.exists():
        raise ImportFailure(f"{dest}: destination already exists")
    for output in outputs:
        target = dest / output.path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(output.content)
        target.chmod(0o755 if output.mode == "100755" else 0o644)
    go_files = [str(dest / output.path) for output in outputs if output.path.endswith(".go")]
    result = subprocess.run(["gofmt", "-w", *go_files], capture_output=True, check=False)
    if result.returncode != 0:
        raise ImportFailure(f"gofmt failed: {result.stderr.decode('utf-8', 'replace').strip()}")


def main(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser(description="Reproduce the pinned git-byline import.")
    parser.add_argument("source", type=Path, help="git-byline clone containing the pinned commit")
    parser.add_argument("dest", type=Path, help="new directory to create")
    args = parser.parse_args(argv)
    source: Path = args.source
    dest: Path = args.dest
    try:
        listing = _git(source, "ls-tree", "-r", "-z", "--full-tree", f"{PIN}^{{commit}}")
        outputs = plan(parse_tree(listing), lambda oid: _git(source, "cat-file", "blob", oid))
        write(dest, outputs)
    except ImportFailure as failure:
        print(f"import-upstream: {failure}", file=sys.stderr)
        return 1
    print(f"import-upstream: wrote {len(outputs)} files from {PIN} to {dest}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
