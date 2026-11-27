"""Behavior of the harness branch publisher against a local bare remote."""

from __future__ import annotations

import subprocess
from typing import TYPE_CHECKING

import pytest

import publish as subject
from publish import Destination, Failure, FailureCode, Output

if TYPE_CHECKING:
    from collections.abc import Callable
    from pathlib import Path


def run_git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=True,
        env={
            "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_AUTHOR_NAME": "Test",
            "GIT_AUTHOR_EMAIL": "test@example.invalid",
            "GIT_COMMITTER_NAME": "Test",
            "GIT_COMMITTER_EMAIL": "test@example.invalid",
            "PATH": "/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin",
        },
    )
    return result.stdout.strip()


@pytest.fixture
def source(tmp_path: Path) -> Path:
    """A clone of a bare remote whose main branch has one commit."""
    remote = tmp_path / "remote.git"
    run_git(tmp_path, "init", "-q", "--bare", "-b", "main", str(remote))
    clone = tmp_path / "source"
    run_git(tmp_path, "clone", "-q", str(remote), str(clone))
    (clone / "README.md").write_text("source\n")
    run_git(clone, "add", "README.md")
    run_git(clone, "commit", "-q", "-m", "source")
    run_git(clone, "push", "-q", "origin", "HEAD:main")
    return clone


def gemini_tree(root: Path, version: str = "1") -> Path:
    tree = root / f"gemini-{version}"
    (tree / "commands").mkdir(parents=True)
    (tree / "commands/git-attributed-setup.toml").write_text(f"prompt = '{version}'\n")
    (tree / "gemini-extension.json").write_text('{"name":"git-attributed"}\n')
    (tree / "LICENSE").write_text("MIT\n")
    (tree / "projection-lock.json").write_text("{}\n")
    return tree


def claude_tree(root: Path) -> Path:
    tree = root / "claude"
    (tree / ".claude-plugin").mkdir(parents=True)
    (tree / ".claude-plugin/marketplace.json").write_text("{}\n")
    (tree / "plugins/git-attributed/.claude-plugin").mkdir(parents=True)
    (tree / "plugins/git-attributed/.claude-plugin/plugin.json").write_text("{}\n")
    (tree / "projection-lock.json").write_text("{}\n")
    return tree


def publication(tree: Path, target: Destination, sha: str) -> subject.Publication:
    parsed = subject.parse([str(tree), target.value, "main", sha])
    assert isinstance(parsed, subject.Publication)
    inspected = subject.inspect(parsed)
    assert isinstance(inspected, subject.Publication)
    return inspected


def test_first_publication_creates_an_orphan_branch_with_provenance(source: Path) -> None:
    head = run_git(source, "rev-parse", "HEAD")
    tree = gemini_tree(source.parent)
    outcome = subject.publish(publication(tree, Destination.GEMINI, head), source)
    assert outcome == Output(f"published harness/gemini from {head}")
    run_git(source, "fetch", "-q", "origin", "harness/gemini")
    files = run_git(source, "ls-tree", "-r", "--name-only", "FETCH_HEAD").splitlines()
    assert files == [
        "LICENSE",
        "commands/git-attributed-setup.toml",
        "gemini-extension.json",
        "projection-lock.json",
    ]
    trailer = run_git(
        source, "log", "-1", "--format=%(trailers:key=Source-Commit,valueonly)", "FETCH_HEAD"
    )
    assert trailer == head
    assert run_git(source, "rev-list", "--count", "FETCH_HEAD") == "1"


def test_republishing_the_same_tree_changes_nothing(source: Path) -> None:
    head = run_git(source, "rev-parse", "HEAD")
    tree = gemini_tree(source.parent)
    assert isinstance(subject.publish(publication(tree, Destination.GEMINI, head), source), Output)
    again = subject.publish(publication(tree, Destination.GEMINI, head), source)
    assert again == Output("harness/gemini is already current.")


def test_a_new_source_commit_advances_the_branch(source: Path) -> None:
    first = run_git(source, "rev-parse", "HEAD")
    subject.publish(publication(gemini_tree(source.parent, "1"), Destination.GEMINI, first), source)
    (source / "README.md").write_text("changed\n")
    run_git(source, "commit", "-q", "-am", "change")
    run_git(source, "push", "-q", "origin", "HEAD:main")
    second = run_git(source, "rev-parse", "HEAD")
    outcome = subject.publish(
        publication(gemini_tree(source.parent, "2"), Destination.GEMINI, second), source
    )
    assert outcome == Output(f"published harness/gemini from {second}")
    run_git(source, "fetch", "-q", "origin", "harness/gemini")
    assert run_git(source, "rev-list", "--count", "FETCH_HEAD") == "2"


def test_a_stale_source_is_skipped(source: Path) -> None:
    stale = run_git(source, "rev-parse", "HEAD")
    (source / "README.md").write_text("newer\n")
    run_git(source, "commit", "-q", "-am", "newer")
    run_git(source, "push", "-q", "origin", "HEAD:main")
    outcome = subject.publish(
        publication(gemini_tree(source.parent), Destination.GEMINI, stale), source
    )
    assert outcome == Output("stale publication skipped")
    assert run_git(source, "ls-remote", "--heads", "origin", "harness/gemini") == ""


def test_a_branch_without_provenance_is_refused(source: Path) -> None:
    head = run_git(source, "rev-parse", "HEAD")
    run_git(source, "push", "-q", "origin", "HEAD:refs/heads/harness/gemini")
    outcome = subject.publish(
        publication(gemini_tree(source.parent), Destination.GEMINI, head), source
    )
    assert outcome == Failure(
        FailureCode.PROVENANCE, "target has no valid Source-Commit provenance"
    )


def test_inspection_accepts_a_complete_claude_tree(tmp_path: Path) -> None:
    tree = claude_tree(tmp_path)
    parsed = subject.parse([str(tree), "harness/claude", "main", "a" * 40])
    assert isinstance(parsed, subject.Publication)
    assert isinstance(subject.inspect(parsed), subject.Publication)


def add_extra_file(tree: Path) -> None:
    (tree / "extra.txt").write_text("x")


def remove_marketplace(tree: Path) -> None:
    (tree / ".claude-plugin/marketplace.json").unlink()


def remove_plugin_manifest(tree: Path) -> None:
    (tree / "plugins/git-attributed/.claude-plugin/plugin.json").unlink()


def add_symlink(tree: Path) -> None:
    (tree / "plugins/link").symlink_to(tree)


@pytest.mark.parametrize(
    ("mutate", "message"),
    [
        (add_extra_file, "projection must contain only"),
        (remove_marketplace, "missing .claude"),
        (remove_plugin_manifest, "missing plugin"),
        (add_symlink, "symlinks"),
    ],
)
def test_inspection_rejects_incomplete_or_unsafe_trees(
    tmp_path: Path, mutate: Callable[[Path], None], message: str
) -> None:
    tree = claude_tree(tmp_path)
    mutate(tree)
    parsed = subject.parse([str(tree), "harness/claude", "main", "a" * 40])
    assert isinstance(parsed, subject.Publication)
    outcome = subject.inspect(parsed)
    assert isinstance(outcome, Failure)
    assert outcome.code is FailureCode.PROJECTION
    assert message in outcome.message


@pytest.mark.parametrize(
    "argv",
    [
        [],
        ["tree", "harness/other", "main", "a" * 40],
        ["tree", "harness/claude", "main", "short"],
        ["tree", "harness/claude", "-main", "a" * 40],
        ["tree", "harness/claude", "main", "a" * 40, "--upload-pack=x"],
    ],
)
def test_invalid_arguments_are_rejected(argv: list[str]) -> None:
    outcome = subject.parse(argv)
    assert isinstance(outcome, Failure)
    assert outcome.code is FailureCode.INPUT
