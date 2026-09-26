#!/usr/bin/env bash
# Checks provenance.sh's verification policy against a stubbed gh.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
# The stub records its arguments and prints $GH_OUT with exit $GH_EXIT.
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CALLS"
printf '%s' "${GH_OUT:-}"
exit "${GH_EXIT:-0}"
STUB
chmod +x "$work/bin/gh"

repo=dannyota/aboutme
say() { printf '%s\n' "$*" >>"$work/said"; }
# shellcheck source=provenance.sh
source "$here/provenance.sh"
PATH="$work/bin:$PATH"
export CALLS=$work/calls

hex=$(printf 'ab%062d' 7)
digest=sha256:$hex
commit=0123456789abcdef0123456789abcdef01234567
statement() { # subject-name hex
  printf '[{"verificationResult":{"statement":{"subject":[{"name":"%s","digest":{"sha256":"%s"}}]}}}]' "$1" "$2"
}

fail() { echo "provenance-test: $*" >&2; exit 1; }
expect() { # case image want(0|1) digest commit
  local got=0
  : >"$CALLS"
  : >"$work/said"
  provenance_verify "$2" "$4" v1.2.3 "$5" || got=1
  [[ $got == "$3" ]] || fail "$1: status $got, want $3"
}

# A verified statement for this image and digest passes, and gh gets the
# whole policy: exact signer identity, tag ref, commit, hosted runner.
export GH_EXIT=0 GH_OUT
GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$hex")
expect ok server 0 "$digest" "$commit"
want="attestation verify oci://ghcr.io/dannyota/aboutme-server@$digest --repo dannyota/aboutme"
want+=" --cert-identity https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v1.2.3"
want+=" --source-ref refs/tags/v1.2.3 --source-digest $commit --deny-self-hosted-runners --format json"
[[ $(cat "$CALLS") == "$want" ]] || fail "ok: gh called with '$(cat "$CALLS")'"

# gh refuses the attestation: the deploy stops.
GH_EXIT=1 GH_OUT=
expect gh_refuses server 1 "$digest" "$commit"
grep -qF "no valid build provenance for server@$digest from v1.2.3" "$work/said" || fail "gh_refuses: no message"
GH_EXIT=0

# One image's digest never passes as another's.
GH_OUT=$(statement ghcr.io/dannyota/aboutme-web "$hex")
expect wrong_name server 1 "$digest" "$commit"
grep -qF "names a different subject" "$work/said" || fail "wrong_name: no message"

# The statement must name this exact digest.
GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$(printf 'cd%062d' 7)")
expect wrong_digest server 1 "$digest" "$commit"

# A name that only starts with the image's name is a different image.
GH_OUT=$(statement ghcr.io/dannyota/aboutme-server-evil "$hex")
expect prefix_name server 1 "$digest" "$commit"

# Empty, non-array, and non-JSON answers fail closed.
for out in '[]' '{}' 'not json' ''; do
  GH_OUT=$out
  expect "empty_answer '$out'" server 1 "$digest" "$commit"
done

# Malformed inputs never reach gh.
GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$hex")
for bad in "sha256:${hex:0:63}" "sha256:${hex^^}" "$hex" "sha512:$hex" "sha256:$hex --repo x/y"; do
  expect "bad_digest '$bad'" server 1 "$bad" "$commit"
  [[ ! -s $CALLS ]] || fail "bad_digest '$bad': gh was called"
done
for bad in "" "${commit:0:39}" "v1.2.3" "${commit^^}"; do
  expect "bad_commit '$bad'" server 1 "$digest" "$bad"
  [[ ! -s $CALLS ]] || fail "bad_commit '$bad': gh was called"
done

echo "provenance-test: ok"
