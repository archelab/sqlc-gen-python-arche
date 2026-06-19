from __future__ import annotations

import os
import pathlib
import typing

import nox
from nox import options

if typing.TYPE_CHECKING:
    import collections.abc

PATH_TO_PROJECT = pathlib.Path(__name__).parent

# The harness sources the broad `pyright` session owns: the noxfile, the helper
# scripts, and the hand-written test files (conftests + test_*.py). It does NOT
# enumerate the generated driver trees directly — those are byte-pinned goldens
# type-checked by the per-driver sessions at their correct Python version floor
# (str_enum needs 3.11 for enum.StrEnum; the fork-wide 3.10 baseline would
# falsely flag it). pyright still follows imports from the hand-written tests
# into the 3.10-safe gens they consume, so reachable generated code stays
# covered without dragging the 3.11-only str_enum gen into the 3.10 sweep.
_HARNESS_TEST_FILES = sorted(
    str(p) for p in (PATH_TO_PROJECT / "test").rglob("*.py") if p.name == "conftest.py" or p.name.startswith("test_")
)
SCRIPT_PATHS = ["noxfile.py", str(PATH_TO_PROJECT / "scripts"), *_HARNESS_TEST_FILES]

DRIVER_PATHS = {
    "asyncpg": PATH_TO_PROJECT / "test" / "driver_asyncpg",
    "aiosqlite": PATH_TO_PROJECT / "test" / "driver_aiosqlite",
    "sqlite3": PATH_TO_PROJECT / "test" / "driver_sqlite3",
    "sqlalchemy": PATH_TO_PROJECT / "test" / "driver_sqlalchemy",
}

SQLC_CONFIGS = ["sqlc.yaml"]

# Per-case directories under test/driver_sqlalchemy that hold their own
# sqlc.yaml (the driver root has no single config — each case configures a
# distinct risky shape). The harness drives them one at a time.
SQLALCHEMY_CASE_DIRS = sorted(p.parent for p in (PATH_TO_PROJECT / "test" / "driver_sqlalchemy").glob("*/sqlc.yaml"))
# enum.StrEnum (the StrEnum emitter base) exists only on Python 3.11+; any
# generated tree that emits a StrEnum must be type-checked at its real version
# floor, not the fork-wide 3.10 baseline (which would falsely flag enum.StrEnum
# as unknown). str_enum is the isolated emitter proof; roundtrip emits a StrEnum
# column too (its live enum round-trip case).
SQLALCHEMY_PYRIGHT_PYVERSION = {"str_enum": "3.11", "roundtrip": "3.11"}

# The vendored reference-shape deletion-proof case (GOAL §6 STEP-B, fork-side): ONE
# consolidated config that exercises every risky shape two external sed/perl
# post-processors patch, generated with ZERO post-processing. The committed
# gen/ tree is the migration baseline; `sqlc diff` regenerates and asserts
# byte-empty (the deletion_proof session).
SHAPE_REPRO_DIR = PATH_TO_PROJECT / "test" / "shape_repro"

options.default_venv_backend = "uv"
options.sessions = [
    "ruff_format",
    "asyncpg",
    "sqlite3",
    "aiosqlite",
    "sqlalchemy",
    "deletion_proof",
    "pyright",
    "ruff",
    "pytest",
]

DEFAULT_POSTGRES_URI = os.getenv("POSTGRES_URI", "postgresql://root:187187@localhost:5432/root")


# uv_sync taken from: https://github.com/hikari-py/hikari/blob/master/pipelines/nox.py#L48
#
# Copyright (c) 2020 Nekokatt
# Copyright (c) 2021-present davfsa
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
def uv_sync(
    session: nox.Session,
    /,
    *,
    include_self: bool = False,
    extras: collections.abc.Sequence[str] = (),
    groups: collections.abc.Sequence[str] = (),
) -> None:
    if extras and not include_self:
        msg = "When specifying extras, set `include_self=True`."
        raise RuntimeError(msg)

    args: list[str] = []
    for extra in extras:
        args.extend(("--extra", extra))

    group_flag = "--group" if include_self else "--only-group"
    for group in groups:
        args.extend((group_flag, group))

    session.run_install(
        "uv",
        "sync",
        "--frozen",
        *args,
        silent=True,
        env={"UV_PROJECT_ENVIRONMENT": session.virtualenv.location},
    )


BUILD_SCRIPT = str(PATH_TO_PROJECT / "scripts" / "build" / "build.sh")


def build_wasm(session: nox.Session) -> None:
    """Build the wasm plugin fresh from the Go source (and copy it beside every
    sqlc.yaml) BEFORE any `sqlc generate`/`sqlc diff`. The wasm is gitignored —
    never committed — so the artifact every golden/deletion-proof session diffs
    against is rebuilt from the CURRENT plugin/ + internal/ source on each run.
    This closes the stale-wasm gap a committed blob would open: there is no path
    where a `sqlc diff` validates against a wasm older than the Go source. The
    build pins GOTOOLCHAIN=go1.24.1 so the sha is reproducible (CI re-derives the
    same sha256 the committed sqlc.yaml pins carry).
    """
    session.run("bash", BUILD_SCRIPT, external=True)


def sqlc_generate(session: nox.Session, driver: str) -> None:
    build_wasm(session)
    with session.chdir(DRIVER_PATHS[driver]):
        for config in SQLC_CONFIGS:
            session.run("sqlc", "generate", "-f", config, external=True)


def sqlc_check(session: nox.Session, driver: str) -> None:
    build_wasm(session)
    with session.chdir(DRIVER_PATHS[driver]):
        for config in SQLC_CONFIGS:
            session.run("sqlc", "diff", "-f", config, external=True)


@nox.session(reuse_venv=True)
def sqlite3(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_generate(session, "sqlite3")
    session.run("pyright", DRIVER_PATHS["sqlite3"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["sqlite3"])


@nox.session(reuse_venv=True)
def sqlite3_check(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_check(session, "sqlite3")
    session.run("pyright", DRIVER_PATHS["sqlite3"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["sqlite3"])


@nox.session(reuse_venv=True)
def aiosqlite(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_generate(session, "aiosqlite")
    session.run("pyright", DRIVER_PATHS["aiosqlite"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["aiosqlite"])


@nox.session(reuse_venv=True)
def aiosqlite_check(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_check(session, "aiosqlite")
    session.run("pyright", DRIVER_PATHS["aiosqlite"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["aiosqlite"])


@nox.session(reuse_venv=True)
def asyncpg(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_generate(session, "asyncpg")
    session.run("pyright", DRIVER_PATHS["asyncpg"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["asyncpg"])


@nox.session(reuse_venv=True)
def asyncpg_check(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    sqlc_check(session, "asyncpg")
    session.run("pyright", DRIVER_PATHS["asyncpg"])
    session.run("ruff", "check", *session.posargs, DRIVER_PATHS["asyncpg"])


def _sqlalchemy_lint_and_type(session: nox.Session) -> None:
    # pyright + ruff over every generated case tree. The package import root is
    # the repo root (cwd), so each case's `from test.driver_sqlalchemy.<case>.gen
    # import models` resolves. str_enum is type-checked at Python 3.11 (its
    # enum.StrEnum floor); the rest at the fork-wide baseline.
    for case_dir in SQLALCHEMY_CASE_DIRS:
        gen_dir = case_dir / "gen"
        pyright_args = ["pyright"]
        if (pyversion := SQLALCHEMY_PYRIGHT_PYVERSION.get(case_dir.name)) is not None:
            pyright_args += ["--pythonversion", pyversion]
        pyright_args.append(str(gen_dir))
        session.run(*pyright_args)
        session.run("ruff", "check", *session.posargs, str(gen_dir))


@nox.session(reuse_venv=True)
def sqlalchemy(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    build_wasm(session)
    for case_dir in SQLALCHEMY_CASE_DIRS:
        with session.chdir(case_dir):
            session.run("sqlc", "generate", "-f", "sqlc.yaml", external=True)
    _sqlalchemy_lint_and_type(session)


@nox.session(reuse_venv=True)
def sqlalchemy_check(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    build_wasm(session)
    for case_dir in SQLALCHEMY_CASE_DIRS:
        with session.chdir(case_dir):
            session.run("sqlc", "diff", "-f", "sqlc.yaml", external=True)
    _sqlalchemy_lint_and_type(session)


@nox.session(reuse_venv=True)
def deletion_proof(session: nox.Session) -> None:
    """GOAL §6 STEP-B fork-side proof: regenerate the vendored reference-shape repro
    through the fork wasm with ZERO post-processing and assert byte-empty vs the
    committed migration baseline (sqlc diff). This proves the generator emits
    EXACTLY what the external sed/perl post-processors used to patch, so deleting
    those post-processors changes nothing. Also pyright+ruff the gen tree at the
    analytics bar (the case sets file_header `# pyright: basic`).
    """
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    build_wasm(session)
    with session.chdir(SHAPE_REPRO_DIR):
        session.run("sqlc", "diff", "-f", "sqlc.yaml", external=True)
    session.run("pyright", str(SHAPE_REPRO_DIR / "gen"))
    session.run("ruff", "check", *session.posargs, str(SHAPE_REPRO_DIR / "gen"))


@nox.session(reuse_venv=True)
def deletion_proof_generate(session: nox.Session) -> None:
    """Regenerate the vendored reference-shape repro IN PLACE (sqlc generate). Use
    to refresh the committed migration baseline after an intentional generator
    change; the byte-empty proof lives in `deletion_proof`.
    """
    uv_sync(session, include_self=True, groups=["pyright", "ruff"])

    build_wasm(session)
    with session.chdir(SHAPE_REPRO_DIR):
        session.run("sqlc", "generate", "-f", "sqlc.yaml", external=True)
    session.run("pyright", str(SHAPE_REPRO_DIR / "gen"))
    session.run("ruff", "check", *session.posargs, str(SHAPE_REPRO_DIR / "gen"))


@nox.session(reuse_venv=True)
def pyright(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pyright"])

    session.run("pyright", *SCRIPT_PATHS)


@nox.session(reuse_venv=True)
def ruff_format(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["ruff"])

    session.run("ruff", "format")
    session.run("ruff", "check", "--select", "I", "--fix")


@nox.session(reuse_venv=True)
def ruff(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["ruff"])

    session.run("ruff", "format")
    session.run("ruff", "check", *session.posargs)


@nox.session(reuse_venv=True)
def ruff_check(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["ruff"])

    session.run("ruff", "format", "--check")
    session.run("ruff", "check", *session.posargs)


PYTEST_RUN_FLAGS = [
    "--showlocals",
    "--show-capture",
    "all",
    f"--db={DEFAULT_POSTGRES_URI}",
]
PYTESTCOVERAGE_FLAGS = [
    "--cov",
    "--cov-config",
    "pyproject.toml",
    "--cov-report",
    "term",
    "--cov-report",
    "html:public",
    "--cov-report",
    "xml",
]


@nox.session(reuse_venv=True)
def pytest(session: nox.Session) -> None:
    uv_sync(session, include_self=True, groups=["pytest"])

    flags = PYTEST_RUN_FLAGS

    if "--coverage" in session.posargs:
        session.posargs.remove("--coverage")
        flags.extend(PYTESTCOVERAGE_FLAGS)

    session.run("pytest", *flags, *session.posargs)
