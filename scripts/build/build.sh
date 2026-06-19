#!/bin/bash
set -euo pipefail

# ──────────────────────────────
# 0) RUN FROM REPO ROOT
# ──────────────────────────────
# Every path below is relative to the repo root (sqlc.yaml, test/...). nox
# sessions invoke this script from inside driver subdirectories, so resolve the
# root from this script's own location (scripts/build/) and cd there. This makes
# `bash scripts/build/build.sh` and an in-session call equivalent.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# ──────────────────────────────
# 1) CONFIGURATION
# ──────────────────────────────
# Top-level driver target dirs that each hold a single sqlc.yaml beside the
# copied wasm. driver_sqlalchemy is special-cased below: it holds per-CASE
# sqlc.yaml files in subdirectories that all point up at one shared wasm.
TARGET_DIRS=(
  "test/driver_asyncpg"
  "test/driver_aiosqlite"
  "test/driver_sqlite3"
  "test/driver_sqlalchemy"
  "test/shape_repro"
)

# ──────────────────────────────
# 2) PORTABLE in-place sed
# ──────────────────────────────
# GNU sed wants `-i` with NO argument; BSD/macOS sed wants `-i ''` (an empty
# backup suffix). Detect once. `\S` is a GNU-ism unsupported by BSD RE, so the
# sha pattern uses an explicit 64-hex-char class that both flavors accept.
SED_INPLACE=(sed -i)
if sed --version >/dev/null 2>&1; then
  : # GNU sed: `sed -i -E ...` is correct as-is.
else
  SED_INPLACE=(sed -i '') # BSD/macOS sed: empty backup suffix required.
fi

patch_sha() {
  local f="$1"
  "${SED_INPLACE[@]}" -E "s/(sha256: )[0-9a-f]{64}/\1$SHA256_HASH/" "$f"
}

# ──────────────────────────────
# 3) PORTABLE sha256
# ──────────────────────────────
# sha256sum is Linux/coreutils; shasum -a 256 is macOS/BSD. Prefer whichever
# exists so the build runs on CI (Linux) and developer Macs alike.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# ──────────────────────────────
# 4) BUILD THE WASM PLUGIN
# ──────────────────────────────
echo "=== Building the Go WASM plugin ================================="
export GOOS=wasip1
export GOARCH=wasm
# Pin the toolchain to the go.mod `go 1.24.1` line. The wasm sha256 is
# toolchain-sensitive: building with the host Go (e.g. 1.26.4) yields a DIFFERENT
# sha than 1.24.1 even with -trimpath. GOTOOLCHAIN forces Go to fetch+use exactly
# go1.24.1 regardless of the host version, so every machine (developer Mac, CI
# runner) produces the SAME sha — which is what makes the committed sqlc.yaml
# sha256 pins a reproducible function of the Go SOURCE rather than the builder's
# Go. Verified: two GOTOOLCHAIN=go1.24.1 builds on a go1.26.4 host are
# byte-identical; the same source under the host toolchain hashes differently.
# Keep this in lockstep with the `go` line in go.mod.
export GOTOOLCHAIN=go1.24.1
# -trimpath strips absolute build paths from the binary so the .wasm is
# reproducible across machines (with the toolchain pinned above — a path-embedded
# OR host-toolchain build would hash differently for every builder).
go build -trimpath -o sqlc-gen-python-arche.wasm plugin/main.go

# ──────────────────────────────
# 5) CALCULATE SHA-256
# ──────────────────────────────
SHA256_HASH=$(sha256_of sqlc-gen-python-arche.wasm)
echo "SHA-256: $SHA256_HASH"

# ──────────────────────────────
# 6) UPDATE ROOT yaml
# ──────────────────────────────
echo "Patching root sqlc.yaml..."
patch_sha sqlc.yaml

# ──────────────────────────────
# 7) PROPAGATE TO TARGET FOLDERS
# ──────────────────────────────
for dir in "${TARGET_DIRS[@]}"; do
  echo "--------------------------------------------------------------"
  echo "  Processing $dir"
  mkdir -p "$dir"
  cp -f sqlc-gen-python-arche.wasm "$dir/"

  # Top-level sqlc.yaml (asyncpg/aiosqlite/sqlite3 have one; sqlalchemy does
  # not — its configs live one level down, patched in step 8).
  if [[ -f "$dir/sqlc.yaml" ]]; then
    echo "  Patching $dir/sqlc.yaml"
    patch_sha "$dir/sqlc.yaml"
  fi
done

# ──────────────────────────────
# 8) RECURSE driver_sqlalchemy per-CASE sqlc.yaml
# ──────────────────────────────
# Every case under test/driver_sqlalchemy/<case>/sqlc.yaml points at the single
# test/driver_sqlalchemy/sqlc-gen-python-arche.wasm copied in step 7, so each
# must carry the same sha. Patch them all.
if [[ -d "test/driver_sqlalchemy" ]]; then
  while IFS= read -r f; do
    echo "  Patching $f"
    patch_sha "$f"
  done < <(find test/driver_sqlalchemy -name sqlc.yaml)
fi

echo "=== All done - every sqlc.yaml now has SHA-256 $SHA256_HASH ======"
