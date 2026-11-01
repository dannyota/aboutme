#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/lib/platform.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/platform-helpers.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
printf abc >"$work/file"
chmod 0600 "$work/file"
mkdir "$work/directory"
chmod 0700 "$work/directory"
ln -s file "$work/link"
[ "$(platform_stat u "$work/file")" = "$(id -u)" ]
[ "$(platform_stat a "$work/file")" = 600 ]
[ "$(platform_stat a "$work/directory")" = 700 ]
[ "$(platform_stat s "$work/file")" = 3 ]
[ "$(platform_stat h "$work/file")" = 1 ]
source <(sed -n '/^browser_stat() {$/,/^}$/p' deploy/dev-https-browser/run.sh)
[ "$(browser_stat u "$work/file")" = "$(id -u)" ]
[ "$(browser_stat a "$work/file")" = 600 ]
[ "$(browser_stat s "$work/file")" = 3 ]
[ "$(browser_stat h "$work/file")" = 1 ]
mkdir "$work/bin"
cat >"$work/bin/uname" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' Darwin
EOF
cat >"$work/bin/stat" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod 0700 "$work/bin/uname" "$work/bin/stat"
(
  export PATH="$work/bin:$PATH"
  [ "$(browser_stat u "$work/file")" = "$(id -u)" ]
  [ "$(browser_stat a "$work/file")" = 600 ]
  [ "$(browser_stat s "$work/file")" = 3 ]
  [ "$(browser_stat h "$work/file")" = 1 ]
)
[ "$(platform_realpath "$work/link")" = "$(cd "$work" && pwd -P)/file" ]
[ "$(platform_stat s "$work/link")" = 4 ]
if platform_stat u "$work/missing" 2>/dev/null || platform_realpath "$work/missing" 2>/dev/null; then
  printf '%s\n' 'missing path accepted' >&2
  exit 1
fi
[ "$(platform_sha256sum "$work/file" | awk '{print $1}')" = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad ]
[ "$(printf abc | platform_sha256sum | awk '{print $1}')" = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad ]
(
  cd "$work"
  names=('back\slash' $'new\nline' $'carriage\rreturn')
  for name in "${names[@]}"; do
    printf abc >"$name"
  done
  digest=ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
  printf '\\%s  back\\\\slash\n\\%s  new\\nline\n\\%s  carriage\\rreturn\n' "$digest" "$digest" "$digest" >expected
  platform_sha256sum "${names[@]}" >actual
  if command -v sha256sum >/dev/null && sha256sum --version 2>/dev/null | head -n 1 | grep -q 'GNU coreutils'; then
    sha256sum "${names[@]}" >gnu
    cmp expected gnu
    cmp gnu actual
  else
    cmp expected actual
  fi
)
mkdir "$work/evidence"
for name in 'proof.a space' 'proof.b space' 'proof.c space'; do
  mkdir "$work/evidence/$name"
done
mkdir "$work/unrelated"
ln -s "$work/unrelated" "$work/evidence/proof.link"
python3 - "$work/evidence" <<'PY'
import os, sys
for name in os.listdir(sys.argv[1]):
    path = os.path.join(sys.argv[1], name)
    if not os.path.islink(path):
        os.utime(path, ns=(1000000000, 1000000000))
PY
platform_prune_evidence "$work/evidence" proof 2
[ ! -e "$work/evidence/proof.a space" ]
[ -d "$work/evidence/proof.b space" ] && [ -d "$work/evidence/proof.c space" ]
[ -L "$work/evidence/proof.link" ] && [ -d "$work/unrelated" ]
mkdir "$work/ties"
mkdir "$work/ties/proof.a" "$work/ties/proof.Z"
python3 - "$work/ties" <<'PY'
import os, sys
for name in os.listdir(sys.argv[1]):
    os.utime(os.path.join(sys.argv[1], name), ns=(1000000000, 1000000000))
PY
first=$(printf '1 %s\n' "$work/ties/proof.a" "$work/ties/proof.Z" | LC_ALL=C sort -rn | head -n 1)
survivor=${first##*/}
[ "$survivor" = proof.a ]
LC_ALL=C platform_prune_evidence "$work/ties" proof 1
[ -d "$work/ties/$survivor" ]
[ ! -e "$work/ties/proof.Z" ]
printf '%s\n' 'platform helper tests: PASS'
