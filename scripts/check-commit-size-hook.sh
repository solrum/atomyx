#!/usr/bin/env bash
#
# Behavior test for scripts/hooks/commit-msg. It builds a throwaway git
# repository, stages changes of known size, and asserts that the hook
# passes under the limit, fails over it, passes over it with a
# Large-Commit trailer, ignores binary files and generated schema
# files, and does nothing during a merge. One case also drives a real
# `git commit` through core.hooksPath to prove the wiring.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
hook="$root/scripts/hooks/commit-msg"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

failures=0

new_repo() {
  rm -rf "$work/repo"
  mkdir "$work/repo"
  git -C "$work/repo" init -q
  git -C "$work/repo" config user.email test@example.com
  git -C "$work/repo" config user.name test
  git -C "$work/repo" config commit.gpgsign false
}

# stage_lines FILE COUNT stages a new text file of COUNT lines.
stage_lines() {
  mkdir -p "$(dirname "$work/repo/$1")"
  seq 1 "$2" > "$work/repo/$1"
  git -C "$work/repo" add "$1"
}

# expect NAME WANT MESSAGE runs the hook in the repo and compares its
# exit status with WANT (0 or 1).
expect() {
  local name="$1" want="$2" message="$3" got=0
  printf '%s\n' "$message" > "$work/msg"
  (cd "$work/repo" && "$hook" "$work/msg") >/dev/null 2>&1 || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL: $name (exit $got, want $want)" >&2
    failures=$((failures + 1))
  else
    echo "PASS: $name"
  fi
}

new_repo
stage_lines a.txt 400
expect "passes at the limit" 0 "subject"

new_repo
stage_lines a.txt 401
expect "fails over the limit" 1 "subject"

new_repo
stage_lines a.txt 401
expect "passes over the limit with the trailer" 0 "$(printf 'subject\n\nLarge-Commit: one generated table')"

new_repo
stage_lines a.txt 401
expect "fails when the trailer has no reason" 1 "$(printf 'subject\n\nLarge-Commit:')"

new_repo
stage_lines a.txt 10
head -c 100000 /dev/urandom > "$work/repo/blob.bin"
printf '\0' >> "$work/repo/blob.bin"
git -C "$work/repo" add blob.bin
expect "skips binary files" 0 "subject"

new_repo
stage_lines core/internal/script/atomyx-script.schema.json 900
expect "skips generated schema files" 0 "subject"

new_repo
stage_lines core/internal/script/other.schema.json 900
expect "counts schema files that are not generated" 1 "subject"

new_repo
stage_lines a.txt 401
touch "$(cd "$work/repo" && git rev-parse --absolute-git-dir)/MERGE_HEAD"
expect "skips during a merge" 0 "subject"

new_repo
mkdir "$work/repo/hooks"
cp "$hook" "$work/repo/hooks/commit-msg"
git -C "$work/repo" config core.hooksPath hooks
stage_lines a.txt 401
if git -C "$work/repo" commit -q -m "subject" >/dev/null 2>&1; then
  echo "FAIL: installed hook rejects an oversized commit" >&2
  failures=$((failures + 1))
elif git -C "$work/repo" commit -q -m "subject" -m "Large-Commit: test" >/dev/null 2>&1; then
  echo "PASS: installed hook rejects an oversized commit and accepts the trailer"
else
  echo "FAIL: installed hook rejects the trailer commit" >&2
  failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
  echo "commit size hook test FAILED: $failures case(s)" >&2
  exit 1
fi

echo "commit size hook test passed."
