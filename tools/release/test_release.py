"""Behavior of the release builder and formula renderer."""

from __future__ import annotations

import hashlib
import io
import subprocess
import tarfile
from pathlib import Path

import pytest

import release as subject
from release import Rejected

REPO = Path(__file__).resolve().parents[2]


@pytest.mark.parametrize("value", ["v0.1.0", "v1.20.3", "v10.0.0"])
def test_accepts_semantic_versions(value: str) -> None:
    assert subject.parse_version(value) == value


@pytest.mark.parametrize("value", ["0.1.0", "v0.1", "v01.0.0", "v0.1.0-rc1", "v0.1.0\n"])
def test_rejects_other_versions(value: str) -> None:
    with pytest.raises(Rejected, match="INVALID_VERSION"):
        subject.parse_version(value)


def test_archive_bytes_depend_only_on_content() -> None:
    files = {"git-attributed": (b"bin", 0o755), "LICENSE": (b"mit", 0o644)}
    first = subject.archive(files, "root", 1_700_000_000)
    second = subject.archive(dict(reversed(list(files.items()))), "root", 1_700_000_000)
    assert first == second
    with tarfile.open(fileobj=io.BytesIO(first)) as tar:
        members = {member.name: member for member in tar.getmembers()}
    assert list(members) == ["root", "root/LICENSE", "root/git-attributed"]
    assert members["root/git-attributed"].mode == 0o755
    assert members["root/LICENSE"].uid == 0
    assert members["root/LICENSE"].mtime == 1_700_000_000


def test_checksums_round_trip() -> None:
    archives = {"b.tar.gz": b"b", "a.tar.gz": b"a"}
    text = subject.checksums(archives)
    assert text.splitlines()[0].endswith("  a.tar.gz")
    assert subject.parse_checksums(text) == {
        name: hashlib.sha256(content).hexdigest() for name, content in archives.items()
    }


def test_malformed_checksums_are_rejected() -> None:
    with pytest.raises(Rejected, match="INVALID_CHECKSUMS"):
        subject.parse_checksums("abc  file\n")


def sums_for(version: str) -> dict[str, str]:
    return {
        subject.archive_name(version, target): f"{index:064x}"
        for index, target in enumerate(subject.TARGETS)
    }


def test_formula_pins_each_platform() -> None:
    rendered = subject.formula("v0.1.0", sums_for("v0.1.0"))
    assert "\n  version " not in rendered
    assert (
        'URL_ROOT = ENV.fetch("HOMEBREW_GIT_ATTRIBUTED_URL_ROOT", '
        '"https://github.com/amichne/attributed/releases/download")'
    ) in rendered
    for index, target in enumerate(subject.TARGETS):
        path = f"v0.1.0/git-attributed_v0.1.0_{target.label}.tar.gz"
        assert f'url "#{{URL_ROOT}}/{path}"\n      sha256 "{index:064x}"' in rendered
    assert 'assert_equal "git-attributed v0.1.0\\n"' in rendered
    assert "#{bin}/git-attributed version" in rendered


def test_formula_requires_every_platform() -> None:
    sums = sums_for("v0.1.0")
    del sums[subject.archive_name("v0.1.0", subject.TARGETS[-1])]
    with pytest.raises(Rejected, match="MISSING_ARCHIVE"):
        subject.formula("v0.1.0", sums)


def test_build_refuses_an_existing_output(tmp_path: Path) -> None:
    with pytest.raises(Rejected, match="OUTPUT_EXISTS"):
        subject.build(REPO, "v0.1.0", tmp_path)


def test_build_produces_reproducible_stamped_archives(tmp_path: Path) -> None:
    first = subject.build(REPO, "v9.9.9", tmp_path / "first")
    second = subject.build(REPO, "v9.9.9", tmp_path / "second")
    assert first == second
    assert sorted(first) == sorted(subject.archive_name("v9.9.9", t) for t in subject.TARGETS)
    sums = subject.parse_checksums((tmp_path / "first" / "checksums.txt").read_text())
    assert set(sums) == set(first)
    host = next(
        t for t in subject.TARGETS if t.label == host_label() and t.goos in ("darwin", "linux")
    )
    extracted = tmp_path / "extracted"
    with tarfile.open(tmp_path / "first" / subject.archive_name("v9.9.9", host)) as tar:
        tar.extractall(extracted, filter="data")
    root = extracted / subject.archive_name("v9.9.9", host).removesuffix(".tar.gz")
    assert sorted(path.name for path in root.iterdir()) == [
        "LICENSE",
        "LICENSE.upstream",
        "README.md",
        "git-attributed",
    ]
    version = subprocess.run(
        [str(root / "git-attributed"), "version"], capture_output=True, text=True, check=True
    )
    assert version.stdout == "git-attributed v9.9.9\n"


def host_label() -> str:
    goos = subprocess.run(["go", "env", "GOOS"], capture_output=True, text=True, check=True)
    goarch = subprocess.run(["go", "env", "GOARCH"], capture_output=True, text=True, check=True)
    return f"{goos.stdout.strip()}_{goarch.stdout.strip()}"
