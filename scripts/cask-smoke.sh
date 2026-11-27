#!/usr/bin/env bash
# Builds a release with GoReleaser from the committed HEAD, serves its
# archives from a local mirror, installs the generated Homebrew cask from a
# temporary tap through HOMEBREW_GIT_ATTRIBUTED_URL_ROOT, and checks the
# installed binary, its licenses, and the path hooks record.
#
#   bash scripts/cask-smoke.sh
#
# Needs goreleaser, brew, git, and python3 on PATH. Nothing is published.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
TAP="attributed-smoke/cask"
PORT="${CASK_SMOKE_PORT:-8765}"
TAG="v0.0.0"
SERVER=""
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_FROM_API=1 HOMEBREW_NO_AUTOREMOVE=1 HOMEBREW_NO_ENV_HINTS=1

fail() { echo "CASK SMOKE FAIL: $*" >&2; exit 1; }
step() { echo; echo "== $*"; }

cleanup() {
  [ -n "$SERVER" ] && kill "$SERVER" 2>/dev/null || true
  brew uninstall --cask --quiet "$TAP/git-attributed" >/dev/null 2>&1 || true
  brew untap --quiet "$TAP" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

step "release build from HEAD"
git clone --quiet "$ROOT" "$WORK/src"
git -C "$WORK/src" tag "$TAG"
(cd "$WORK/src" && HOMEBREW_TAP_TOKEN=unused goreleaser release --clean --skip=publish >"$WORK/goreleaser.log" 2>&1) ||
  { cat "$WORK/goreleaser.log"; fail "goreleaser release"; }
CASK="$WORK/src/dist/homebrew/Casks/git-attributed.rb"
[ -f "$CASK" ] || fail "no cask generated"

step "local mirror"
mkdir -p "$WORK/mirror/$TAG"
cp "$WORK/src/dist/"*.tar.gz "$WORK/mirror/$TAG/"
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WORK/mirror" >"$WORK/server.log" 2>&1 &
SERVER=$!
for _ in $(seq 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$PORT/" 2>/dev/null && break
  sleep 0.1
done

step "install the cask through the mirror"
brew tap-new --quiet --no-git "$TAP" >/dev/null
TAP_DIR="$(brew --repository "$TAP")"
mkdir -p "$TAP_DIR/Casks"
cp "$CASK" "$TAP_DIR/Casks/"
HOMEBREW_GIT_ATTRIBUTED_URL_ROOT="http://127.0.0.1:$PORT" brew install --cask "$TAP/git-attributed" ||
  fail "brew install"
grep -q "GET /$TAG/git-attributed_${TAG}_" "$WORK/server.log" || fail "the mirror served no archive"

step "installed binary"
LINK="$(brew --prefix)/bin/git-attributed"
[ "$("$LINK" version)" = "git-attributed $TAG" ] || fail "unexpected version: $("$LINK" version)"
"$LINK" license | grep -q "Comarch S.A." || fail "license notice missing"
STAGED="$(dirname "$(readlink "$LINK")")"
for file in LICENSE LICENSE.upstream; do
  [ -f "$STAGED/$file" ] || fail "$file missing from $STAGED"
done

step "hooks record the stable bin/ link"
git init --quiet "$WORK/repo"
(cd "$WORK/repo" && PATH="$(dirname "$LINK"):$PATH" git attributed install-hooks --agent none --git --local-notes >/dev/null) ||
  fail "install-hooks"
grep -q "'$LINK'" "$WORK/repo/.git/hooks/post-commit" || fail "post-commit does not use $LINK"

echo
echo "CASK SMOKE OK"
