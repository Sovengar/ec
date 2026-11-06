#!/usr/bin/env bash
# Regenerates testdata/playground: a git repo stopped in a merge with many
# conflict blocks per file and several files. diff3 style, so every block has a
# ||||||| base section.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/testdata/playground"
feature="$root/testdata/playground-feature"
gen="$root/scripts/gen_conflicts.py"

rm -rf "$out" "$feature"
mkdir -p "$out"
cd "$out"

git init -q
git config user.email demo@example.com
git config user.name demo
git config merge.conflictstyle diff3

python3 "$gen" base "$out"
git add -A
git commit -qm base
base="$(git rev-parse HEAD)"
git branch feature "$base"

python3 "$gen" ours "$out"
git add -A
git commit -qm "ours changes"

git worktree add -q "$feature" feature
python3 "$gen" theirs "$feature"
git -C "$feature" add -A
git -C "$feature" commit -qm "theirs changes"
git worktree remove --force "$feature"

git merge feature >/dev/null 2>&1 || true
echo "fixture: $out"
git status --short
