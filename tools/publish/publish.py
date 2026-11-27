#!/usr/bin/env -S uv run --locked --script
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
"""Publish a projected provider tree to its harness branch with source provenance.

Adapted from amichne/slopsentral tools/publish.py at
939be0332f4ce46d0923fee14560d781377373f8.

    uv run --locked --script tools/publish/publish.py PROJECTION TARGET SOURCE SHA [REMOTE]

PROJECTION is a tree from `project.py publish --provider`. TARGET is one of
harness/claude, harness/factory, or harness/gemini. SOURCE is the branch
that SHA must still head. Each publication commit carries a Source-Commit
trailer, and a target only moves forward along SOURCE's history.
"""

from __future__ import annotations

import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import assert_never


class Destination(StrEnum):
    CLAUDE = "harness/claude"
    FACTORY = "harness/factory"
    GEMINI = "harness/gemini"


class FailureCode(StrEnum):
    INPUT = "invalid-input"
    PROJECTION = "invalid-projection"
    PROCESS = "git-failed"
    PROVENANCE = "provenance-rejected"
    FILESYSTEM = "filesystem-failed"
    CLEANUP = "cleanup-failed"


@dataclass(frozen=True, slots=True)
class Failure:
    code: FailureCode
    message: str


@dataclass(frozen=True, slots=True)
class Output:
    text: str


@dataclass(frozen=True, slots=True)
class Shape:
    """The top-level entries and required files of one provider tree."""

    entries: frozenset[str]
    required: tuple[str, ...]
    plugin_manifest: str | None


SHAPES: dict[Destination, Shape] = {
    Destination.CLAUDE: Shape(
        frozenset({".claude-plugin", "plugins", "projection-lock.json"}),
        (".claude-plugin/marketplace.json",),
        ".claude-plugin/plugin.json",
    ),
    Destination.FACTORY: Shape(
        frozenset({".factory-plugin", "plugins", "projection-lock.json"}),
        (".factory-plugin/marketplace.json",),
        ".factory-plugin/plugin.json",
    ),
    Destination.GEMINI: Shape(
        frozenset({"gemini-extension.json", "commands", "LICENSE", "projection-lock.json"}),
        ("gemini-extension.json", "commands/git-attributed-setup.toml"),
        None,
    ),
}


@dataclass(frozen=True, slots=True)
class Publication:
    projection: Path
    target: Destination
    source_branch: str
    source: str
    remote: str


class Freshness(StrEnum):
    CURRENT = "current"
    STALE = "stale"


def parse(arguments: list[str]) -> Publication | Failure:
    match arguments:
        case [projection, target_name, source_branch, sha, *optional] if len(optional) <= 1:
            pass
        case _:
            return Failure(
                FailureCode.INPUT, "usage: publish.py PROJECTION TARGET SOURCE SHA [REMOTE]"
            )
    if target_name not in {destination.value for destination in Destination}:
        return Failure(FailureCode.INPUT, "unsupported target branch")
    remote = optional[0] if optional else "origin"
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        return Failure(FailureCode.INPUT, "source SHA must be 40 lowercase hexadecimal characters")
    if not re.fullmatch(r"[A-Za-z0-9._-]+", remote) or remote.startswith("-"):
        return Failure(FailureCode.INPUT, "invalid remote name")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/-]*", source_branch):
        return Failure(FailureCode.INPUT, "invalid source branch")
    return Publication(
        Path(projection).absolute(), Destination(target_name), source_branch, sha, remote
    )


def inspect(publication: Publication) -> Publication | Failure:
    directory = publication.projection
    shape = SHAPES[publication.target]
    if directory.is_symlink() or not directory.is_dir():
        return Failure(FailureCode.PROJECTION, "projection must be a regular directory")
    if frozenset(path.name for path in directory.iterdir()) != shape.entries:
        expected = ", ".join(sorted(shape.entries))
        return Failure(FailureCode.PROJECTION, f"projection must contain only {expected}")
    if any(
        path.is_symlink() or path.name == ".git" or not (path.is_file() or path.is_dir())
        for path in directory.rglob("*")
    ):
        return Failure(
            FailureCode.PROJECTION,
            "projection contains symlinks, Git metadata, or nonregular files",
        )
    for required in shape.required:
        if not (directory / required).is_file():
            return Failure(FailureCode.PROJECTION, f"missing {required}")
    if shape.plugin_manifest is not None:
        bundles = tuple((directory / "plugins").iterdir())
        if not bundles or any(not (bundle / shape.plugin_manifest).is_file() for bundle in bundles):
            return Failure(FailureCode.PROJECTION, f"missing plugin {shape.plugin_manifest}")
    return publication


def git(repo: Path, *arguments: str) -> Output | Failure:
    try:
        result = subprocess.run(
            ["git", "-C", str(repo), *arguments], capture_output=True, text=True, check=False
        )
    except OSError:
        return Failure(FailureCode.PROCESS, "Git unavailable")
    if result.returncode:
        return Failure(
            FailureCode.PROCESS, f"Git {arguments[0]} failed with exit {result.returncode}"
        )
    return Output(result.stdout.strip())


def freshness(repo: Path, publication: Publication) -> Freshness | Failure:
    ref = f"refs/remotes/{publication.remote}/{publication.source_branch}"
    fetched = git(
        repo,
        "fetch",
        "--no-tags",
        publication.remote,
        f"+refs/heads/{publication.source_branch}:{ref}",
    )
    if isinstance(fetched, Failure):
        return fetched
    current = git(repo, "rev-parse", ref)
    if isinstance(current, Failure):
        return current
    if current.text != publication.source:
        print(f"::notice::{publication.source_branch} advanced; skipping stale publication.")
        return Freshness.STALE
    return Freshness.CURRENT


def provenance(repo: Path, publication: Publication, target_ref: str) -> Output | Failure:
    refreshed = freshness(repo, publication)
    if isinstance(refreshed, Failure):
        return refreshed
    if refreshed is Freshness.STALE:
        return Failure(FailureCode.PROVENANCE, "source advanced during provenance check")
    trailer = git(repo, "log", "-1", "--format=%(trailers:key=Source-Commit,valueonly)", target_ref)
    if isinstance(trailer, Failure):
        return trailer
    if not re.fullmatch(r"[0-9a-f]{40}", trailer.text):
        return Failure(FailureCode.PROVENANCE, "target has no valid Source-Commit provenance")
    ancestry = git(repo, "merge-base", "--is-ancestor", trailer.text, publication.source)
    if isinstance(ancestry, Failure):
        return Failure(
            FailureCode.PROVENANCE, f"refusing to move {publication.target.value} backward"
        )
    return Output(target_ref)


def prepare(repo: Path, worktree: Path, publication: Publication) -> Output | Failure:
    target_ref = f"refs/remotes/{publication.remote}/{publication.target.value}"
    listing = git(
        repo, "ls-remote", "--heads", publication.remote, f"refs/heads/{publication.target.value}"
    )
    if isinstance(listing, Failure):
        return listing
    if listing.text:
        fetched = git(
            repo,
            "fetch",
            "--no-tags",
            publication.remote,
            f"+refs/heads/{publication.target.value}:{target_ref}",
        )
        if isinstance(fetched, Failure):
            return fetched
        verified = provenance(repo, publication, target_ref)
        return (
            verified
            if isinstance(verified, Failure)
            else git(repo, "worktree", "add", "--detach", str(worktree), verified.text)
        )
    created = git(repo, "worktree", "add", "--detach", str(worktree), publication.source)
    if isinstance(created, Failure):
        return created
    return git(worktree, "switch", "--orphan", publication.target.value)


def copy_payload(worktree: Path, publication: Publication) -> Output | Failure:
    removed = git(worktree, "rm", "-rf", "--ignore-unmatch", "--", ".")
    if isinstance(removed, Failure):
        return removed
    for entry in sorted(SHAPES[publication.target].entries):
        source = publication.projection / entry
        destination = worktree / entry
        if destination.is_symlink():
            return Failure(FailureCode.PROJECTION, "unsafe destination")
        if destination.is_dir():
            shutil.rmtree(destination)
        elif destination.exists():
            destination.unlink()
        if source.is_dir():
            _ = shutil.copytree(source, destination)
        else:
            _ = shutil.copyfile(source, destination)
    staged = git(worktree, "add", "-f", "-A", "--", ".")
    if isinstance(staged, Failure):
        return staged
    return git(worktree, "diff", "--cached", "--check")


def finish(repo: Path, worktree: Path, publication: Publication) -> Output | Failure:
    copied = copy_payload(worktree, publication)
    if isinstance(copied, Failure):
        return copied
    diff = git(worktree, "diff", "--cached", "--name-only")
    if isinstance(diff, Failure):
        return diff
    if not diff.text:
        return Output(f"{publication.target.value} is already current.")
    committed = git(
        worktree,
        "-c",
        "user.name=github-actions[bot]",
        "-c",
        "user.email=github-actions[bot]@users.noreply.github.com",
        "commit",
        "-m",
        f"chore: publish {publication.target.value}",
        "-m",
        f"Source-Commit: {publication.source}",
    )
    if isinstance(committed, Failure):
        return committed
    return push_current(repo, worktree, publication)


def push_current(repo: Path, worktree: Path, publication: Publication) -> Output | Failure:
    """Push the publication commit only while the source still heads its branch."""
    current = freshness(repo, publication)
    if isinstance(current, Failure):
        return current
    if current is Freshness.STALE:
        return Output("stale publication skipped")
    pushed = git(
        worktree, "push", publication.remote, f"HEAD:refs/heads/{publication.target.value}"
    )
    if isinstance(pushed, Failure):
        return pushed
    return Output(f"published {publication.target.value} from {publication.source}")


def publish(publication: Publication, repo: Path) -> Output | Failure:
    current = freshness(repo, publication)
    if isinstance(current, Failure):
        return current
    if current is Freshness.STALE:
        return Output("stale publication skipped")
    with tempfile.TemporaryDirectory(prefix="attributed-publish-") as temporary:
        worktree = Path(temporary) / "payload"
        outcome: Output | Failure
        try:
            prepared = prepare(repo, worktree, publication)
            outcome = (
                prepared if isinstance(prepared, Failure) else finish(repo, worktree, publication)
            )
        finally:
            cleanup = (
                git(repo, "worktree", "remove", "--force", str(worktree))
                if worktree.exists()
                else Output("")
            )
        if isinstance(cleanup, Failure):
            return Failure(
                FailureCode.CLEANUP,
                "worktree cleanup failed; publication outcome requires inspection",
            )
        return outcome


def main(argv: list[str], repo: Path | None = None) -> int:
    parsed = parse(argv)
    try:
        validated = parsed if isinstance(parsed, Failure) else inspect(parsed)
        result = (
            validated if isinstance(validated, Failure) else publish(validated, repo or Path.cwd())
        )
    except OSError:
        result = Failure(
            FailureCode.FILESYSTEM, "filesystem operation failed; inspect publication state"
        )
    match result:
        case Failure(code, message):
            print(f"publish: stage={code.value}: {message}", file=sys.stderr)
            return 1
        case Output(text):
            print(text)
            return 0
    assert_never(result)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
