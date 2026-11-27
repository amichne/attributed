"""Behavior of the upstream import codemod."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest

import import_upstream as subject
from import_upstream import Entry, ImportFailure

if TYPE_CHECKING:
    from pathlib import Path


def entry(path: str, mode: subject.Mode = "100644") -> Entry:
    return Entry(mode, "0" * 40, path)


@pytest.mark.parametrize(
    ("before", "after"),
    [
        (
            "github.com/comarch/git-byline/internal/model",
            "github.com/amichne/attributed/internal/model",
        ),
        ('metadata["comarch.git-byline"]', 'metadata["amichne.attributed"]'),
        ("uses: comarch/git-byline@v1", "uses: amichne/attributed@v1"),
        ("git-byline checkpoint", "git-attributed checkpoint"),
        ("GIT_BYLINE_VERSION", "GIT_ATTRIBUTED_VERSION"),
        ("readByline", "readAttributed"),
        ("refs/notes/byline-stash", "refs/notes/attributed-stash"),
        ('"name": "Wojciech Guziak (Comarch S.A.)"', '"name": "Austin Michne"'),
    ],
)
def test_rename_maps_each_upstream_form(before: str, after: str) -> None:
    assert subject.rename(before) == after


@pytest.mark.parametrize(
    "path", [".github/workflows/ci.yml", "docs", "LICENSE", "tools/validate/main.go"]
)
def test_excluded_paths(path: str) -> None:
    assert subject.excluded(path)


@pytest.mark.parametrize(
    "path", ["docsx/a.md", "marketplace/git-byline/LICENSE", "tools/covermerge/main.go"]
)
def test_included_paths(path: str) -> None:
    assert not subject.excluded(path)


def test_transform_renames_path_and_content_and_keeps_mode() -> None:
    output = subject.transform(entry("cmd/git-byline/main.go", "100755"), b"// git-byline entry\n")
    assert (output.path, output.mode, output.content) == (
        "cmd/git-attributed/main.go",
        "100755",
        b"// git-attributed entry\n",
    )


def test_verbatim_file_keeps_its_notice() -> None:
    notice = b"Copyright (c) 2026 Comarch S.A.\n"
    output = subject.transform(entry("marketplace/git-byline/LICENSE"), notice)
    assert (output.path, output.content) == ("marketplace/git-attributed/LICENSE", notice)


def test_residual_upstream_name_fails_with_location() -> None:
    with pytest.raises(ImportFailure, match=r"a\.md:2: .*'Comarch'"):
        subject.transform(entry("a.md"), b"ok\nby Comarch S.A.\n")


def test_binary_content_fails() -> None:
    with pytest.raises(ImportFailure, match="not UTF-8"):
        subject.transform(entry("a.gif"), b"\xff\xfe")


def test_derived_fixture_values_are_replaced() -> None:
    path = "internal/interop/testdata/gitai-export.golden"
    output = subject.transform(entry(path), b"h_6e3b111ed570c7 t_661df9ba1b7b4e git-byline")
    assert output.content == b"h_d550ff2fbe4333 t_26f7b413a2fd2f git-attributed"


def test_missing_derived_value_fails() -> None:
    with pytest.raises(ImportFailure, match="h_6e3b111ed570c7 is absent"):
        subject.transform(
            entry("internal/interop/testdata/gitai-export.golden"), b"t_661df9ba1b7b4e"
        )


def test_plan_skips_exclusions_and_rejects_collisions() -> None:
    def read(_: str) -> bytes:
        return b"x\n"

    kept = subject.plan([entry("AGENTS.md"), entry("a/byline.go")], read)
    assert [output.path for output in kept] == ["a/attributed.go"]
    with pytest.raises(ImportFailure, match="two upstream paths"):
        subject.plan([entry("a/byline.go"), entry("a/attributed.go")], read)


@pytest.mark.parametrize("record", [b"120000 blob abc\tlink", b"160000 commit abc\tsub"])
def test_parse_tree_rejects_links_and_submodules(record: bytes) -> None:
    with pytest.raises(ImportFailure, match="unsupported tree entry"):
        subject.parse_tree(record + b"\0")


def test_parse_tree_reads_regular_and_executable_blobs() -> None:
    listing = b"100644 blob aaa\ta b.txt\x00100755 blob bbb\trun.sh\x00"
    assert subject.parse_tree(listing) == [
        Entry("100644", "aaa", "a b.txt"),
        Entry("100755", "bbb", "run.sh"),
    ]


def test_write_refuses_existing_destination(tmp_path: Path) -> None:
    with pytest.raises(ImportFailure, match="already exists"):
        subject.write(tmp_path, [])
