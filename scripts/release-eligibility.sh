#!/usr/bin/env bash
# Shared publication and deployment gate. Prints only the eligible commit.
set -euo pipefail

fail() { printf 'release-eligibility: %s\n' "$*" >&2; exit 1; }
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo=dannyota/aboutme
checkout=0
if [[ ${1:-} == --deploy-checkout ]]; then checkout=1; shift; fi
[[ $# -ge 1 && $# -le 2 ]] || fail 'usage: release-eligibility.sh [--deploy-checkout] <tag> [expected-commit]'
tag=$1
expected=${2:-}
[[ $tag =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] ||
  fail 'tag must be strict vMAJOR.MINOR.PATCH'
[[ -z $expected || $expected =~ ^[0-9a-f]{40}$ ]] || fail 'expected commit must be a full SHA'

if ((checkout)); then
  state=$(git -C "$root" status --porcelain --untracked-files=normal) || fail 'cannot inspect deployment checkout'
  [[ -z $state ]] || fail 'deployment checkout must be clean, including untracked files'
  branch=$(git -C "$root" symbolic-ref --quiet --short HEAD) || fail 'deployment checkout must be on main'
  [[ $branch == main ]] || fail 'deployment checkout must be on main'
fi

# Fetch from the release repository, even if origin points at a fork. Explicit
# refs refresh main and the tag; a replaced local tag fails instead of moving.
git -C "$root" fetch -q --no-tags "https://github.com/$repo.git" \
  '+refs/heads/main:refs/remotes/origin/main' "refs/tags/$tag:refs/tags/$tag" ||
  fail 'cannot fetch main and the release tag'
commit=$(git -C "$root" rev-parse --verify "refs/tags/$tag^{commit}") || fail 'tag does not resolve to a commit'
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail 'tag did not resolve to a full commit SHA'
[[ -z $expected || $commit == "$expected" ]] || fail 'tag commit differs from the event commit'
git -C "$root" merge-base --is-ancestor "$commit" refs/remotes/origin/main || fail 'tag is not on main'
if ((checkout)); then
  head=$(git -C "$root" rev-parse --verify HEAD) || fail 'cannot resolve deployment checkout'
  main=$(git -C "$root" rev-parse --verify refs/remotes/origin/main) || fail 'cannot resolve current main'
  [[ $head == "$main" ]] || fail 'deployment checkout must equal current origin/main'
fi

# Do not filter for success in the API: a newer pending or failed run must
# block an older success. Branch and dispatch results never authorize release.
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
