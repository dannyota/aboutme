#!/usr/bin/env bash
# Builds the pinned FOP 2.3 container (Containerfile) and renders every *.fo
# fixture in this directory to its *.pdf, with Apache FOP 2.3
# (docs/design/linkedin-import.md, "Tests"). The PDFs this writes are
# generated once and committed; CI never regenerates them, and this script
# refuses to run there. See README.md for the exact command this is meant to
# be run under (a shared local-check lock and a hard memory cap), the pinned
# image digest, and the FOP and font checksums.
set -euo pipefail

if [[ -n "${CI:-}" ]]; then
  echo "generate.sh must not run in CI: the fixture PDFs are generated" \
    "once, by hand, and committed; see README.md." >&2
  exit 1
fi

cd "$(dirname "${BASH_SOURCE[0]}")"

image="aboutme-linkedin-fop:2.3"

echo "building $image"
podman build --tag "$image" --file Containerfile .

shopt -s nullglob
sources=(*.fo)
shopt -u nullglob
if [[ ${#sources[@]} -eq 0 ]]; then
  echo "no *.fo sources found in $(pwd)" >&2
  exit 1
fi

for source in "${sources[@]}"; do
  name="${source%.fo}"
  echo "rendering $name.pdf"
  podman run \
    --rm \
    --network=none \
    --memory=2g \
    --memory-swap=2g \
    --volume "$(pwd):/work/src:ro,Z" \
    --volume "$(pwd):/work/out:rw,Z" \
    "$image" \
    -c /work/src/fop.xconf \
    -nocs \
    -fo "/work/src/$source" \
    -pdf "/work/out/$name.pdf"
done

echo "rendered ${#sources[@]} fixture(s)"
