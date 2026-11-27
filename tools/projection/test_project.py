"""Behavior of the integration projector."""

from __future__ import annotations

import hashlib
import json
import shutil
import subprocess
from pathlib import Path

import pytest

import project as subject
from project import Frozen, Plan, PlannedFile, ProviderFile, Rejected

REPO = Path(__file__).resolve().parents[2]
NO_PROVIDERS: dict[str, tuple[ProviderFile, ...]] = {name: () for name in subject.PROVIDERS}


def planned(path: str, text: str = "x\n", seals: tuple[str, ...] = ()) -> PlannedFile:
    return PlannedFile(path, text, seals)


def plan(*files: PlannedFile, **providers: tuple[ProviderFile, ...]) -> Plan:
    return Plan(files, NO_PROVIDERS | providers)


def raw_plan(files: list[object], providers: object | None = None) -> str:
    default: dict[str, list[object]] = {name: [] for name in subject.PROVIDERS}
    return json.dumps({"files": files, "providers": default if providers is None else providers})


def rejected_code(error: pytest.ExceptionInfo[Rejected]) -> tuple[str, str]:
    return error.value.stage, error.value.code


@pytest.mark.parametrize("path", ["marketplace/a.json", "commands/x/y.toml"])
def test_safe_path_accepts_owned_relative_paths(path: str) -> None:
    assert subject.safe_path(path)


@pytest.mark.parametrize(
    "path",
    ["../x", "/marketplace/a", "marketplace", "other/a", "marketplace/./a", "marketplace//a"],
)
def test_safe_path_rejects_escapes_and_foreign_roots(path: str) -> None:
    assert not subject.safe_path(path)


@pytest.mark.parametrize(
    "raw",
    [
        "not json",
        "[]",
        '{"files": [], "providers": {}, "extra": 1}',
        '{"files": []}',
        raw_plan([{"path": "marketplace/a", "text": "x"}]),
        raw_plan([{"path": "marketplace/a", "text": "x", "seals": [1]}]),
        raw_plan([], {"claude": [], "factory": []}),
        raw_plan([], {"claude": [{"path": "a"}], "factory": [], "gemini": []}),
    ],
)
def test_parse_plan_rejects_malformed_plans(raw: str) -> None:
    with pytest.raises(Rejected) as error:
        subject.parse_plan(raw)
    assert rejected_code(error) == ("validate", "INVALID_PLAN")


def test_parse_plan_reads_files_and_providers_in_order() -> None:
    raw = raw_plan(
        [{"path": "marketplace/a", "text": "t", "seals": ["marketplace/b"]}],
        {"claude": [{"path": "x/y", "text": "z"}], "factory": [], "gemini": []},
    )
    assert subject.parse_plan(raw) == plan(
        planned("marketplace/a", "t", ("marketplace/b",)),
        claude=(ProviderFile("x/y", "z"),),
    )


@pytest.mark.parametrize(
    ("candidate", "code"),
    [
        (plan(planned(subject.LOCK_PATH)), "UNSAFE_PATH"),
        (plan(planned("elsewhere/a")), "UNSAFE_PATH"),
        (plan(planned("marketplace/a"), planned("marketplace/a")), "DUPLICATE_PATH"),
        (plan(claude=(ProviderFile("../escape", "x"),)), "UNSAFE_PATH"),
        (plan(gemini=(ProviderFile(subject.PROVIDER_LOCK_PATH, "x"),)), "UNSAFE_PATH"),
        (plan(factory=(ProviderFile("a", "x"), ProviderFile("a", "y"))), "DUPLICATE_PATH"),
    ],
)
def test_validate_plan_rejects_unsafe_or_duplicate_paths(candidate: Plan, code: str) -> None:
    with pytest.raises(Rejected) as error:
        subject.validate_plan(candidate, Frozen({}))
    assert rejected_code(error) == ("validate", code)


def test_validate_plan_rejects_an_undeclared_asset() -> None:
    frozen = Frozen({"integrations/assets/marketplace/stray.md": b"x"})
    with pytest.raises(Rejected) as error:
        subject.validate_plan(plan(planned("marketplace/a")), frozen)
    assert rejected_code(error) == ("validate", "UNDECLARED_ASSET")


def test_seal_digests_hash_the_referenced_text() -> None:
    files = [
        planned("marketplace/setup.md", seals=("marketplace/p.ts",)),
        planned("marketplace/p.ts", "code"),
    ]
    assert subject.seal_digests(files) == {"marketplace/p.ts": hashlib.sha256(b"code").hexdigest()}


@pytest.mark.parametrize(
    ("files", "code"),
    [
        ([planned("marketplace/a", seals=("marketplace/missing",))], "MISSING_REFERENCE"),
        (
            [
                planned("marketplace/a", seals=("marketplace/b",)),
                planned("marketplace/b", seals=("marketplace/c",)),
                planned("marketplace/c"),
            ],
            "SEAL_CHAIN",
        ),
    ],
)
def test_seal_digests_reject_missing_and_chained_references(
    files: list[PlannedFile], code: str
) -> None:
    with pytest.raises(Rejected) as error:
        subject.seal_digests(files)
    assert rejected_code(error) == ("seal", code)


def sealed_pair(after_text: str) -> tuple[list[PlannedFile], list[PlannedFile], dict[str, str]]:
    digest = hashlib.sha256(b"code").hexdigest()
    before = [
        planned("marketplace/s.md", f"sum={subject.ZERO_DIGEST}\n", ("marketplace/p.ts",)),
        planned("marketplace/p.ts", "code"),
    ]
    after = [
        planned("marketplace/s.md", after_text.format(digest=digest), ("marketplace/p.ts",)),
        planned("marketplace/p.ts", "code"),
    ]
    return before, after, {"marketplace/p.ts": digest}


def test_verify_sealed_accepts_a_placeholder_replacement() -> None:
    subject.verify_sealed(*sealed_pair("sum={digest}\n"))


def test_verify_sealed_rejects_any_other_change() -> None:
    with pytest.raises(Rejected) as error:
        subject.verify_sealed(*sealed_pair("sum={digest}\nextra\n"))
    assert rejected_code(error) == ("seal", "SEAL_UNSTABLE")


def test_render_tree_adds_a_sorted_lock() -> None:
    frozen = Frozen({"integrations/a": b"a"})
    tree = subject.render_tree([planned("marketplace/b", "b"), planned("commands/a", "a")], frozen)
    lock = json.loads(tree[subject.LOCK_PATH])
    assert lock["sourceSha"] == frozen.digest
    assert list(lock["files"]) == ["commands/a", "marketplace/b"]
    assert lock["files"]["commands/a"] == hashlib.sha256(b"a").hexdigest()


def test_drift_names_missing_unowned_and_changed_paths() -> None:
    report = subject.drift(
        {"marketplace/a": b"1", "marketplace/b": b"2"},
        {"marketplace/b": b"3", "marketplace/c": b"4"},
    )
    assert report == ["missing marketplace/a", "changed marketplace/b", "unowned marketplace/c"]


def test_frozen_digest_separates_path_and_content() -> None:
    assert Frozen({"a": b"bc"}).digest != Frozen({"ab": b"c"}).digest


def test_publish_refuses_an_existing_destination(tmp_path: Path) -> None:
    (tmp_path / "out").mkdir()
    with pytest.raises(Rejected) as error:
        subject.publish({"marketplace/a": b"x"}, tmp_path / "out")
    assert rejected_code(error) == ("publish", "OUTPUT_EXISTS")


def test_publish_refuses_a_missing_parent(tmp_path: Path) -> None:
    with pytest.raises(Rejected) as error:
        subject.publish({"marketplace/a": b"x"}, tmp_path / "absent" / "out")
    assert rejected_code(error) == ("publish", "UNSAFE_PATH")


def test_publish_writes_the_tree_and_removes_its_staging(tmp_path: Path) -> None:
    subject.publish({"marketplace/a": b"x"}, tmp_path / "out")
    assert (tmp_path / "out/marketplace/a").read_bytes() == b"x"
    assert sorted(path.name for path in tmp_path.iterdir()) == ["out"]


def test_freeze_rejects_a_symlink_source(tmp_path: Path) -> None:
    (tmp_path / "integrations").mkdir()
    (tmp_path / "integrations/link").symlink_to("/etc/hosts")
    with pytest.raises(Rejected) as error:
        subject.freeze(tmp_path)
    assert rejected_code(error) == ("freeze", "NON_REGULAR_SOURCE")


# ---- evaluation with the pinned Pkl -----------------------------------------


def pinned_pkl() -> str | None:
    pkl = shutil.which("pkl")
    if pkl is None:
        return None
    version = subprocess.run([pkl, "--version"], capture_output=True, text=True, check=False)
    return pkl if version.stdout.startswith(f"Pkl {subject.PKL_VERSION} ") else None


requires_pkl = pytest.mark.skipif(
    pinned_pkl() is None, reason=f"Pkl {subject.PKL_VERSION} unavailable"
)


@pytest.fixture
def sources(tmp_path: Path) -> Path:
    """A repository copy with only the projection inputs and outputs."""
    for relative in ("integrations", "marketplace", "commands"):
        shutil.copytree(REPO / relative, tmp_path / relative)
    for relative in subject.SOURCE_FILES:
        (tmp_path / relative).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(REPO / relative, tmp_path / relative)
    return tmp_path


@requires_pkl
def test_projection_matches_the_committed_tree(sources: Path) -> None:
    tree = subject.project(sources, "pkl").repository
    assert subject.drift(tree, subject.committed_tree(sources)) == []


@requires_pkl
def test_provider_trees_copy_the_projected_plugin(sources: Path) -> None:
    projected = subject.project(sources, "pkl")
    repository = projected.repository
    for provider, manifest in (("claude", ".claude-plugin"), ("factory", ".factory-plugin")):
        tree = projected.providers[provider]
        listing = json.loads(tree[f"{manifest}/marketplace.json"])
        assert listing["name"] == "attributed"
        [entry] = listing["plugins"]
        root = entry["source"].removeprefix("./")
        assert root == "plugins/git-attributed"
        for name in (
            f"{manifest}/plugin.json",
            "README.md",
            "LICENSE",
            "commands/git-attributed-setup.md",
            "skills/git-attributed/SKILL.md",
        ):
            assert tree[f"{root}/{name}"] == repository[f"marketplace/git-attributed/{name}"]
    gemini = projected.providers["gemini"]
    assert json.loads(gemini["gemini-extension.json"])["name"] == "git-attributed"
    assert (
        gemini["commands/git-attributed-setup.toml"]
        == repository["commands/git-attributed-setup.toml"]
    )


@requires_pkl
def test_each_provider_tree_carries_a_complete_lock(sources: Path) -> None:
    for tree in subject.project(sources, "pkl").providers.values():
        lock = json.loads(tree[subject.PROVIDER_LOCK_PATH])
        assert set(lock["files"]) == set(tree) - {subject.PROVIDER_LOCK_PATH}
        for path, digest in lock["files"].items():
            assert hashlib.sha256(tree[path]).hexdigest() == digest


@requires_pkl
def test_projection_seals_the_opencode_plugin_digest(sources: Path) -> None:
    tree = subject.project(sources, "pkl").repository
    plugin = tree["marketplace/harness/opencode/git-attributed.ts"]
    setup = tree["marketplace/harness/opencode/git-attributed-setup.md"].decode()
    assert setup.count(hashlib.sha256(plugin).hexdigest()) == 2
    assert subject.ZERO_DIGEST not in setup


@requires_pkl
def test_a_changed_matcher_drifts_only_its_harness(sources: Path) -> None:
    source = sources / "integrations/Integrations.pkl"
    text = source.read_text()
    source.write_text(
        text.replace('editMatcher = "write_file|replace"', 'editMatcher = "write_file"')
    )
    tree = subject.project(sources, "pkl").repository
    assert subject.drift(tree, subject.committed_tree(sources)) == [
        "changed marketplace/harness/gemini/settings.json",
        "changed marketplace/projection-lock.json",
    ]


@requires_pkl
def test_an_undeclared_asset_stops_projection(sources: Path) -> None:
    (sources / "integrations/assets/marketplace/stray.md").write_text("stray\n")
    with pytest.raises(Rejected) as error:
        subject.project(sources, "pkl")
    assert rejected_code(error) == ("validate", "UNDECLARED_ASSET")


@requires_pkl
def test_a_seal_to_an_unknown_file_stops_projection(sources: Path) -> None:
    asset = sources / "integrations/assets/marketplace/harness/README.md"
    asset.write_text(asset.read_text() + "@@sha256:marketplace/absent.ts@@\n")
    with pytest.raises(Rejected) as error:
        subject.project(sources, "pkl")
    assert rejected_code(error) == ("seal", "MISSING_REFERENCE")


@requires_pkl
def test_a_type_violation_names_its_model_location(sources: Path) -> None:
    source = sources / "integrations/Integrations.pkl"
    source.write_text(source.read_text().replace("timeout = 30000", "timeout = 0"))
    with pytest.raises(Rejected) as error:
        subject.project(sources, "pkl")
    assert rejected_code(error) == ("evaluate", "EVALUATION_FAILED")
    assert "secrets" not in error.value.detail


def test_an_unpinned_pkl_is_refused(tmp_path: Path) -> None:
    fake = tmp_path / "pkl"
    fake.write_text("#!/bin/sh\necho 'Pkl 0.1.0 (fake)'\n")
    fake.chmod(0o755)
    with pytest.raises(Rejected) as error:
        subject.resolve_pkl(str(fake))
    assert rejected_code(error) == ("evaluate", "PKL_VERSION")
