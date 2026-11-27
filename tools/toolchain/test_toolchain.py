"""Behavior of the pinned toolchain bootstrapper."""

from __future__ import annotations

import hashlib
import json
from typing import TYPE_CHECKING

import pytest

import toolchain as subject
from toolchain import Failure

if TYPE_CHECKING:
    from pathlib import Path

SUPPORTED = ("darwin-arm64", "linux-x64", "linux-arm64")


def test_committed_manifest_pins_every_tool_for_supported_platforms() -> None:
    tools = subject.load_manifest(subject.MANIFEST.read_text())
    assert not isinstance(tools, Failure)
    assert set(tools) == {"uv", "pkl"}
    assert tools["pkl"].version == "0.33.0"
    assert tools["pkl"].environment == "PKL_BIN"
    for tool in tools.values():
        for host in SUPPORTED:
            assert host in tool.assets


@pytest.mark.parametrize(
    "change",
    [
        {"url": "http://github.com/a/b"},
        {"url": "https://example.com/a/b"},
        {"sha256": "abc"},
        {"format": "rar"},
        {"executable": "../pkl"},
    ],
)
def test_manifest_rejects_unsafe_assets(change: dict[str, str]) -> None:
    asset = {
        "url": "https://github.com/apple/pkl/releases/download/0.33.0/pkl-linux-amd64",
        "sha256": "0" * 64,
        "format": "binary",
        "executable": "pkl",
    } | change
    manifest = {
        "schemaVersion": 1,
        "tools": {"pkl": {"version": "0.33.0", "profiles": ["core"], "assets": {"any": asset}}},
    }
    assert subject.load_manifest(json.dumps(manifest)) == Failure("INVALID_MANIFEST")


def tool_entry(tmp_path: Path, content: bytes) -> subject.Entry:
    asset = subject.Asset(
        "https://github.com/example/tool/releases/download/1/tool",
        hashlib.sha256(content).hexdigest(),
        "binary",
        "tool",
    )
    tool = subject.Tool("1", ("core",), {"any": asset}, "TOOL_BIN")
    return subject.Entry("tool", tool, asset, tmp_path / "cache" / "tool-1")


def test_fetch_admits_only_the_pinned_digest(tmp_path: Path) -> None:
    entry = tool_entry(tmp_path, b"expected")

    def tampered(_: str, path: Path) -> Failure | None:
        path.write_bytes(b"tampered")
        return None

    assert subject.fetch(entry, tampered) == Failure("DIGEST_MISMATCH", "tool")
    assert not entry.ready()

    def genuine(_: str, path: Path) -> Failure | None:
        path.write_bytes(b"expected")
        return None

    assert subject.fetch(entry, genuine) is None
    assert entry.ready()
    assert entry.executable.read_bytes() == b"expected"


def test_environment_exports_the_executable(tmp_path: Path) -> None:
    entry = tool_entry(tmp_path, b"x")
    variables, directories = subject.environment([entry])
    assert variables == {"TOOL_BIN": str(entry.executable)}
    assert directories == [str(entry.executable.parent)]


def test_unsupported_platform_is_rejected(tmp_path: Path) -> None:
    tools = subject.load_manifest(subject.MANIFEST.read_text())
    assert not isinstance(tools, Failure)
    selected = subject.entries(tools, ["pkl"], "core", "darwin-x64", tmp_path)
    assert selected == Failure("UNSUPPORTED_PLATFORM", "pkl on darwin-x64")


@pytest.mark.parametrize("argv", [[], ["bogus"], ["run"], ["fetch", "--profile", "x"]])
def test_invalid_invocations_are_rejected(argv: list[str]) -> None:
    outcome = subject.run(argv)
    assert isinstance(outcome, Failure)
    assert outcome.reason == "INVALID_INVOCATION"
