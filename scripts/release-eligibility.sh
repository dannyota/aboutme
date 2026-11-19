#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'release-eligibility: %s\n' "$*" >&2; exit 1; }
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repository_file=scripts/lib/repository.sh
repository_pattern=$'^# shellcheck shell=bash disable=SC2034\nowner=(dannyota|dntmakers)\nname=aboutme\n$'
parse_owner() {
  [[ $1 =~ $repository_pattern ]] ||
    fail "$2 must hold exactly the shell directive, owner=dannyota or owner=dntmakers, and name=aboutme"
  printf '%s\n' "${BASH_REMATCH[1]}"
}
checkout_owner() {
  local path=$root/$repository_file text
  if [[ ! -e $path && ! -L $path ]]; then printf 'dannyota\n'; return; fi
  [[ -f $path && ! -L $path && ! -x $path ]] || fail "$repository_file is not a regular file"
  text=$(cat -- "$path" && printf x) || fail "cannot read $repository_file"
  parse_owner "${text%x}" "$repository_file"
}
commit_owner() {
  local entry text
  [[ $1 =~ ^[0-9a-f]{40}$ ]] || fail 'owner commit must be a full SHA'
  entry=$(git -C "$root" ls-tree --full-tree "$1^{commit}" -- "$repository_file") ||
    fail "cannot inspect $repository_file at $1"
  if [[ -z $entry ]]; then printf 'dannyota\n'; return; fi
  [[ $entry =~ ^100644\ blob\ [0-9a-f]{40,64}$'\t'"$repository_file"$ ]] ||
    fail "$repository_file at $1 is not a regular file"
  text=$(git -C "$root" show "$1:$repository_file" && printf x) || fail "cannot read $repository_file at $1"
  parse_owner "${text%x}" "$repository_file at $1"
}
checkout_on_main() {
  state=$(git -C "$root" status --porcelain --untracked-files=normal) || fail 'cannot inspect deployment checkout'
  [[ -z $state ]] || fail 'deployment checkout must be clean, including untracked files'
  branch=$(git -C "$root" symbolic-ref --quiet --short HEAD) || fail 'deployment checkout must be on main'
  [[ $branch == main ]] || fail 'deployment checkout must be on main'
}
fetch_main() {
  git -C "$root" fetch -q --no-tags "https://github.com/$repo.git" '+refs/heads/main:refs/remotes/origin/main' "$@"
}
checkout_current() {
  head=$(git -C "$root" rev-parse --verify HEAD) || fail 'cannot resolve deployment checkout'
  main=$(git -C "$root" rev-parse --verify refs/remotes/origin/main) || fail 'cannot resolve current main'
  [[ $head == "$main" ]] || fail 'deployment checkout must equal current origin/main'
}

if [[ ${1:-} == --owner ]]; then
  case $# in
    1) checkout_owner ;;
    2) commit_owner "$2" ;;
    *) fail 'usage: release-eligibility.sh --owner [commit]' ;;
  esac
  exit 0
fi
repo=$(checkout_owner)/aboutme
checkout=0
if [[ ${1:-} == --deploy-checkout ]]; then checkout=1; shift; fi
if ((checkout && $# == 0)); then
  checkout_on_main
  fetch_main || fail 'cannot fetch main'
  checkout_current
  exit 0
fi
[[ $# -ge 1 && $# -le 3 ]] ||
  fail 'usage: release-eligibility.sh [--deploy-checkout] <tag> [expected-commit [expected-owner]] | --deploy-checkout | --owner [commit]'
tag=$1
expected=${2:-}
[[ $tag =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] ||
  fail 'tag must be strict vMAJOR.MINOR.PATCH'
[[ -z $expected || $expected =~ ^[0-9a-f]{40}$ ]] || fail 'expected commit must be a full SHA'
(($# < 3)) || [[ $3 =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}$ ]] || fail 'expected owner must be a GitHub login'

if ((checkout)); then checkout_on_main; fi
fetch_main "refs/tags/$tag:refs/tags/$tag" || fail 'cannot fetch main and the release tag'
commit=$(git -C "$root" rev-parse --verify "refs/tags/$tag^{commit}") || fail 'tag does not resolve to a commit'
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail 'tag did not resolve to a full commit SHA'
[[ -z $expected || $commit == "$expected" ]] || fail 'tag commit differs from the event commit'
git -C "$root" merge-base --is-ancestor "$commit" refs/remotes/origin/main || fail 'tag is not on main'
if ((checkout)); then checkout_current; fi
if (($# == 3)); then
  owner=$(commit_owner "$commit")
  [[ $owner == "$3" ]] || fail "$tag names $owner in $repository_file, not the publishing owner $3"
fi

ci=$(gh run list --repo "$repo" --workflow ci.yml --commit "$commit" --event push --branch main \
  --json databaseId,conclusion,status,event,headBranch,headSha --limit 20 |
  jq -er --arg c "$commit" '
    [.[] | select(.event == "push" and .headBranch == "main" and .headSha == $c)]
    | sort_by(.databaseId) | last
    | if . == null then "none"
      elif .status != "completed" then (.status // "unknown")
      else (.conclusion // "unknown") end') || fail 'cannot read main CI evidence'
[[ $ci == success ]] || fail "latest push run of ci.yml on main for $tag ($commit) is $ci"
printf '%s\n' "$commit"
