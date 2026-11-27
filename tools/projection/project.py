#!/usr/bin/env -S uv run --locked --script
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
"""Project Attributed's agent integrations into marketplace/ and commands/.

    uv run --locked --script tools/projection/project.py check
    uv run --locked --script tools/projection/project.py publish --out DIR

`check` projects into a temporary tree and compares it with the committed
marketplace/ and commands/ trees. `publish` writes a complete tree at DIR,
whose parent must exist and which must not exist. Success prints CHECKED or
PROJECTED on stdout. Failure prints PROJECTION_REJECTED with a stage and a
code on stderr.
"""

from __future__ import annotations

import argparse
import ctypes
import errno
import hashlib
import json
import os
import secrets
import shutil
import stat
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import TYPE_CHECKING, Final, Literal, cast

if TYPE_CHECKING:
    from collections.abc import Mapping, Sequence

PKL_VERSION: Final = "0.33.0"
OWNED_ROOTS: Final = ("marketplace", "commands")
LOCK_PATH: Final = "marketplace/projection-lock.json"
PROVIDERS: Final = ("claude", "factory", "gemini")
PROVIDER_LOCK_PATH: Final = "projection-lock.json"
ASSET_ROOT: Final = "integrations/assets/"
SOURCE_DIRS: Final = ("integrations",)
SOURCE_FILES: Final = (
    "LICENSE.upstream",
    "tools/projection/Project.pkl",
    "tools/projection/Schema.pkl",
)
ZERO_DIGEST: Final = "0" * 64
EVALUATION_SECONDS: Final = 60

type Stage = Literal["freeze", "evaluate", "validate", "seal", "stage", "publish", "check"]


class Rejected(Exception):
    """Projection stopped at a named stage with a finite code."""

    def __init__(self, stage: Stage, code: str, detail: str = "") -> None:
        super().__init__(f"{stage}:{code}")
        self.stage: Stage = stage
        self.code = code
        self.detail = detail


@dataclass(frozen=True)
class Frozen:
    """Source bytes read once, keyed by repository-relative POSIX path."""

    files: Mapping[str, bytes]

    @property
    def digest(self) -> str:
        hasher = hashlib.sha256()
        for path in sorted(self.files):
            hasher.update(path.encode("utf-8") + b"\0" + self.files[path] + b"\0")
        return hasher.hexdigest()


@dataclass(frozen=True)
class PlannedFile:
    path: str
    text: str
    seals: tuple[str, ...]


@dataclass(frozen=True)
class ProviderFile:
    """One file of a provider branch tree, at a path relative to that tree."""

    path: str
    text: str


@dataclass(frozen=True)
class Plan:
    files: tuple[PlannedFile, ...]
    providers: Mapping[str, tuple[ProviderFile, ...]]


@dataclass(frozen=True)
class Projected:
    """The repository tree and one tree per provider branch, each with a lock."""

    repository: Mapping[str, bytes]
    providers: Mapping[str, Mapping[str, bytes]]


# ---- pure core -------------------------------------------------------------


def relative_path(path: str) -> bool:
    pure = PurePosixPath(path)
    return (
        not pure.is_absolute()
        and str(pure) == path
        and all(part not in ("", ".", "..") for part in pure.parts)
    )


def safe_path(path: str) -> bool:
    pure = PurePosixPath(path)
    return relative_path(path) and len(pure.parts) >= 2 and pure.parts[0] in OWNED_ROOTS


def _object(value: object, keys: set[str]) -> dict[str, object]:
    """Narrow decoded JSON to an object with exactly these keys."""
    if not isinstance(value, dict):
        raise Rejected("validate", "INVALID_PLAN")
    fields = cast("dict[object, object]", value)
    if set(fields) != keys:
        raise Rejected("validate", "INVALID_PLAN")
    return {str(key): item for key, item in fields.items()}


def _list(value: object) -> list[object]:
    if not isinstance(value, list):
        raise Rejected("validate", "INVALID_PLAN")
    return cast("list[object]", value)


def _string(value: object) -> str:
    if not isinstance(value, str):
        raise Rejected("validate", "INVALID_PLAN")
    return value


def parse_plan(raw: str) -> Plan:
    try:
        document: object = json.loads(raw)
    except json.JSONDecodeError as error:
        raise Rejected("validate", "INVALID_PLAN") from error
    top = _object(document, {"files", "providers"})
    planned: list[PlannedFile] = []
    for value in _list(top["files"]):
        entry = _object(value, {"path", "text", "seals"})
        seals = tuple(_string(item) for item in _list(entry["seals"]))
        planned.append(PlannedFile(_string(entry["path"]), _string(entry["text"]), seals))
    providers: dict[str, tuple[ProviderFile, ...]] = {}
    for name, listing in _object(top["providers"], set(PROVIDERS)).items():
        files: list[ProviderFile] = []
        for value in _list(listing):
            entry = _object(value, {"path", "text"})
            files.append(ProviderFile(_string(entry["path"]), _string(entry["text"])))
        providers[name] = tuple(files)
    return Plan(tuple(planned), providers)


def validate_plan(plan: Plan, frozen: Frozen) -> None:
    paths = [item.path for item in plan.files]
    for path in paths:
        if not safe_path(path) or path == LOCK_PATH:
            raise Rejected("validate", "UNSAFE_PATH", path)
    if len(paths) != len(set(paths)):
        raise Rejected("validate", "DUPLICATE_PATH")
    declared = set(paths)
    for source in frozen.files:
        if source.startswith(ASSET_ROOT) and source.removeprefix(ASSET_ROOT) not in declared:
            raise Rejected("validate", "UNDECLARED_ASSET", source)
    for name, files in plan.providers.items():
        provider_paths = [item.path for item in files]
        for path in provider_paths:
            if not relative_path(path) or path == PROVIDER_LOCK_PATH:
                raise Rejected("validate", "UNSAFE_PATH", f"{name}:{path}")
        if len(provider_paths) != len(set(provider_paths)):
            raise Rejected("validate", "DUPLICATE_PATH", name)


def seal_digests(planned: Sequence[PlannedFile]) -> dict[str, str]:
    """Digest every sealed reference from the unsealed plan."""
    by_path = {item.path: item for item in planned}
    digests: dict[str, str] = {}
    for item in planned:
        for reference in item.seals:
            target = by_path.get(reference)
            if target is None:
                raise Rejected("seal", "MISSING_REFERENCE", reference)
            if target.seals:
                raise Rejected("seal", "SEAL_CHAIN", reference)
            digests[reference] = sha256(target.text)
    return digests


def verify_sealed(
    unsealed: Sequence[PlannedFile], sealed: Sequence[PlannedFile], digests: Mapping[str, str]
) -> None:
    """Sealing may change only the zero placeholders of sealing files."""
    if [item.path for item in unsealed] != [item.path for item in sealed]:
        raise Rejected("seal", "SEAL_UNSTABLE")
    for before, after in zip(unsealed, sealed, strict=True):
        if before.seals != after.seals:
            raise Rejected("seal", "SEAL_UNSTABLE", before.path)
        restored = after.text
        for reference in before.seals:
            restored = restored.replace(digests[reference], ZERO_DIGEST)
        if restored != before.text:
            raise Rejected("seal", "SEAL_UNSTABLE", before.path)
    for reference, digest in digests.items():
        if sha256(next(item.text for item in sealed if item.path == reference)) != digest:
            raise Rejected("seal", "SEAL_UNSTABLE", reference)


def with_lock(tree: dict[str, bytes], frozen: Frozen, lock_path: str) -> dict[str, bytes]:
    """Add a lock that records the source digest and every file digest."""
    lock = {
        "version": 1,
        "pkl": PKL_VERSION,
        "sourceSha": frozen.digest,
        "files": {path: hashlib.sha256(tree[path]).hexdigest() for path in sorted(tree)},
    }
    tree[lock_path] = (json.dumps(lock, indent=2, sort_keys=True) + "\n").encode("utf-8")
    return tree


def render_tree(planned: Sequence[PlannedFile], frozen: Frozen) -> dict[str, bytes]:
    return with_lock({item.path: item.text.encode("utf-8") for item in planned}, frozen, LOCK_PATH)


def render_provider(files: Sequence[ProviderFile], frozen: Frozen) -> dict[str, bytes]:
    tree = {item.path: item.text.encode("utf-8") for item in files}
    return with_lock(tree, frozen, PROVIDER_LOCK_PATH)


def drift(projected: Mapping[str, bytes], committed: Mapping[str, bytes]) -> list[str]:
    report: list[str] = []
    for path in sorted(set(projected) | set(committed)):
        if path not in committed:
            report.append(f"missing {path}")
        elif path not in projected:
            report.append(f"unowned {path}")
        elif projected[path] != committed[path]:
            report.append(f"changed {path}")
    return report


def sha256(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


# ---- effects ---------------------------------------------------------------


def read_regular_tree(root: Path, relative: str) -> dict[str, bytes]:
    files: dict[str, bytes] = {}
    base = root / relative
    if not base.is_dir() or base.is_symlink():
        raise Rejected("freeze", "MISSING_SOURCE", relative)
    for directory, dirnames, filenames in os.walk(base):
        for name in dirnames + filenames:
            path = Path(directory) / name
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode) or not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise Rejected("freeze", "NON_REGULAR_SOURCE", str(path.relative_to(root)))
        for name in filenames:
            if name.endswith(".pyc"):
                continue
            path = Path(directory) / name
            files[path.relative_to(root).as_posix()] = path.read_bytes()
        dirnames[:] = [name for name in dirnames if name != "__pycache__"]
    return files


def freeze(repo: Path) -> Frozen:
    files: dict[str, bytes] = {}
    for relative in SOURCE_DIRS:
        files.update(read_regular_tree(repo, relative))
    for relative in SOURCE_FILES:
        path = repo / relative
        if path.is_symlink() or not path.is_file():
            raise Rejected("freeze", "MISSING_SOURCE", relative)
        files[relative] = path.read_bytes()
    return Frozen(files)


def materialize(root: Path, files: Mapping[str, bytes]) -> None:
    for relative, content in files.items():
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        target.chmod(0o644)


def resolve_pkl(pkl: str) -> str:
    """The absolute path of the pinned Pkl executable."""
    resolved = shutil.which(pkl)
    if resolved is None:
        raise Rejected("evaluate", "PKL_UNAVAILABLE")
    try:
        result = subprocess.run(
            [resolved, "--version"], capture_output=True, text=True, check=False
        )
    except OSError as error:
        raise Rejected("evaluate", "PKL_UNAVAILABLE") from error
    if result.returncode != 0 or not result.stdout.startswith(f"Pkl {PKL_VERSION} "):
        raise Rejected("evaluate", "PKL_VERSION", result.stdout.split("(")[0].strip())
    return str(Path(resolved).absolute())


def evaluate(pkl: str, frozen_root: Path, digests: Mapping[str, str]) -> Plan:
    command = [
        pkl,
        "eval",
        "--root-dir",
        str(frozen_root),
        "--allowed-modules",
        "pkl:,file:",
        "--allowed-resources",
        "file:,prop:",
        "--timeout",
        str(EVALUATION_SECONDS),
        "--no-cache",
    ]
    for reference, digest in sorted(digests.items()):
        command += ["-p", f"sha256.{reference}={digest}"]
    command.append(str(frozen_root / "tools/projection/Project.pkl"))
    home = frozen_root.parent / "home"
    home.mkdir(exist_ok=True)
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            check=False,
            env={"HOME": str(home)},
            timeout=EVALUATION_SECONDS + 10,
        )
    except subprocess.TimeoutExpired as error:
        raise Rejected("evaluate", "EVALUATION_TIMEOUT") from error
    except OSError as error:
        raise Rejected("evaluate", "PKL_UNAVAILABLE") from error
    if result.returncode != 0:
        raise Rejected("evaluate", "EVALUATION_FAILED", pkl_location(result.stderr, frozen_root))
    return parse_plan(result.stdout)


def pkl_location(stderr: str, frozen_root: Path) -> str:
    """The first model location in a Pkl diagnostic, without source payloads."""
    prefix = f"file://{frozen_root}/"
    for line in stderr.splitlines():
        if line.startswith("at ") and prefix in line:
            return line.split(prefix, 1)[1].rstrip(")")
    return ""


def project(repo: Path, pkl: str) -> Projected:
    frozen = freeze(repo)
    pkl = resolve_pkl(pkl)
    with tempfile.TemporaryDirectory(prefix="attributed-projection-") as scratch:
        frozen_root = Path(scratch) / "source"
        materialize(frozen_root, frozen.files)
        unsealed = evaluate(pkl, frozen_root, {})
        validate_plan(unsealed, frozen)
        digests = seal_digests(unsealed.files)
        sealed = evaluate(pkl, frozen_root, digests) if digests else unsealed
        validate_plan(sealed, frozen)
        verify_sealed(unsealed.files, sealed.files, digests)
    if freeze(repo).digest != frozen.digest:
        raise Rejected("freeze", "CHANGED_INPUT")
    return Projected(
        render_tree(sealed.files, frozen),
        {name: render_provider(files, frozen) for name, files in sealed.providers.items()},
    )


def committed_tree(repo: Path) -> dict[str, bytes]:
    files: dict[str, bytes] = {}
    for root in OWNED_ROOTS:
        if (repo / root).exists():
            files.update(read_regular_tree(repo, root))
    return files


def publish_directory(stage_dir: Path, target: Path) -> None:
    """Rename stage_dir to target atomically, refusing to replace target."""
    libc = ctypes.CDLL(None, use_errno=True)
    try:
        if sys.platform == "darwin":
            rename = libc.renamex_np
            rename.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint]
            # apple-oss-distributions/xnu bsd/sys/stdio.h: RENAME_EXCL.
            args: tuple[object, ...] = (os.fsencode(stage_dir), os.fsencode(target), 0x4)
        elif sys.platform == "linux":
            rename = libc.renameat2
            rename.argtypes = [
                ctypes.c_int,
                ctypes.c_char_p,
                ctypes.c_int,
                ctypes.c_char_p,
                ctypes.c_uint,
            ]
            # renameat2(2): AT_FDCWD and RENAME_NOREPLACE.
            args = (-100, os.fsencode(stage_dir), -100, os.fsencode(target), 1)
        else:
            raise Rejected("publish", "ATOMIC_PUBLISH_UNAVAILABLE")
    except AttributeError as error:
        raise Rejected("publish", "ATOMIC_PUBLISH_UNAVAILABLE") from error
    rename.restype = ctypes.c_int
    if rename(*args) == 0:
        return
    failure = ctypes.get_errno()
    if failure in (errno.EEXIST, errno.ENOTEMPTY):
        raise Rejected("publish", "OUTPUT_EXISTS")
    if failure in (errno.ENOSYS, errno.EINVAL, errno.ENOTSUP):
        raise Rejected("publish", "ATOMIC_PUBLISH_UNAVAILABLE")
    raise Rejected("publish", "WRITE_FAILED")


def publish(tree: Mapping[str, bytes], out: Path) -> None:
    out = out.absolute()
    if not out.parent.is_dir():
        raise Rejected("publish", "UNSAFE_PATH", str(out.parent))
    if out.exists() or out.is_symlink():
        raise Rejected("publish", "OUTPUT_EXISTS")
    stage_dir = out.parent / f".{out.name}.staging-{secrets.token_hex(8)}"
    try:
        stage_dir.mkdir()
    except OSError as error:
        raise Rejected("stage", "WRITE_FAILED") from error
    try:
        materialize(stage_dir, tree)
        publish_directory(stage_dir, out)
    except OSError as error:
        raise Rejected("stage", "WRITE_FAILED") from error
    finally:
        if stage_dir.exists():
            shutil.rmtree(stage_dir)


def main(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser(description="Project Attributed's agent integrations.")
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--pkl", default=os.environ.get("PKL_BIN", "pkl"))
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("check", help="compare the projection with marketplace/ and commands/")
    publish_parser = commands.add_parser("publish", help="write the projection to a new directory")
    publish_parser.add_argument("--out", type=Path, required=True)
    publish_parser.add_argument(
        "--provider",
        choices=PROVIDERS,
        help="write that provider branch tree instead of marketplace/ and commands/",
    )
    args = parser.parse_args(argv)
    repo: Path = args.repo
    pkl: str = args.pkl
    try:
        projected = project(repo, pkl)
        tree = projected.repository
        if args.command == "publish":
            out: Path = args.out
            provider: str | None = args.provider
            publish(tree if provider is None else projected.providers[provider], out)
            print(f"PROJECTED {out}")
            return 0
        report = drift(tree, committed_tree(repo))
        if report:
            for line in report[:50]:
                print(line, file=sys.stderr)
            raise Rejected("check", "DRIFT", f"{len(report)} paths")
    except Rejected as rejected:
        detail = f" detail={rejected.detail}" if rejected.detail else ""
        print(
            f"PROJECTION_REJECTED stage={rejected.stage} code={rejected.code}{detail}",
            file=sys.stderr,
        )
        return 1
    print(f"CHECKED {len(tree)} files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
