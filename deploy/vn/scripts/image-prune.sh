#!/usr/bin/env bash
# shellcheck disable=SC2154

prune_refs() {
  [[ -e $1 || -L $1 ]] || return 0
  grep -ohE 'ghcr\.io/(dannyota|dntmakers)/aboutme-[a-z]+@sha256:[0-9a-f]{64}' -- "$1" || (($? == 1))
}

step_prune_images() {
  local refs='' out file ref id status listing removed=0 kept=0
  local -A keep=()
  checkpoint
  for file in "$release_json" "$state/release.previous.json" "$maintenance_json"; do
    out=$(prune_refs "$file") || die "warning: could not read $file; removed no images"
    refs+=$out$'\n'
  done
  mountpoint -q "$data" || die "warning: $data is not mounted; removed no images"
  listing=''
  if [[ -e $operations_dir || -L $operations_dir ]]; then
    [[ -d $operations_dir && ! -L $operations_dir ]] || die "warning: could not list $operations_dir; removed no images"
    listing=$(find "$operations_dir" -mindepth 2 -maxdepth 2 -name state.json 2>/dev/null) ||
      die "warning: could not list $operations_dir; removed no images"
  fi
  while IFS= read -r file; do
    [[ -n $file ]] || continue
    out=$(prune_refs "$file") || die "warning: could not read $file; removed no images"
    refs+=$out$'\n'
  done <<<"$listing"
  [[ -d $units ]] || die "warning: could not list $units; removed no images"
  listing=$(find "$units" -mindepth 1 -maxdepth 1 -name '*.container' 2>/dev/null) ||
    die "warning: could not list $units; removed no images"
  while IFS= read -r file; do
    [[ -n $file ]] || continue
    out=$(sed -n 's/^Image=//p' -- "$file" 2>/dev/null) || die "warning: could not read $file; removed no images"
    refs+=$out$'\n'
  done <<<"$listing"
  listing=$(podman ps -a --no-trunc --format '{{.ImageID}}' 2>/dev/null) ||
    die "warning: could not list containers; removed no images"
  while read -r id; do
    [[ -n $id ]] || continue
    id=${id#sha256:}
    [[ $id =~ ^[0-9a-f]{64}$ ]] || die "warning: a container has a malformed image ID; removed no images"
    keep[$id]=1
  done <<<"$listing"
  while read -r ref; do
    [[ -n $ref ]] || continue
    status=0
    podman image exists -- "$ref" >/dev/null 2>&1 || status=$?
    case $status in
      0) ;;
      1) continue ;;
      *) die "warning: could not check $ref; removed no images" ;;
    esac
    id=$(podman image inspect --format '{{.Id}}' -- "$ref" 2>/dev/null) ||
      die "warning: could not inspect $ref; removed no images"
    id=${id#sha256:}
    [[ $id =~ ^[0-9a-f]{64}$ ]] || die "warning: $ref has a malformed image ID; removed no images"
    keep[$id]=1
  done <<<"$refs"
  out=$(podman images --format json 2>/dev/null) || die "warning: could not list images; removed no images"
  listing=$(jq -r 'if type == "array" then .[] else error("not a list") end
    | (.Id | ltrimstr("sha256:") | if test("^[0-9a-f]{64}$") then . else error("malformed ID") end) as $id
    | (.RepoDigests // [])[]
    | select(test("^ghcr\\.io/(dannyota|dntmakers)/aboutme-[a-z]+@sha256:[0-9a-f]{64}$")) | "\($id) \(.)"' <<<"$out" 2>/dev/null) ||
    die "warning: could not read the image list; removed no images"
  while read -r id ref; do
    [[ -n $id ]] || continue
    if [[ -n ${keep[$id]:-} ]]; then
      kept=$((kept + 1))
    elif podman image rm --no-prune -- "$ref" >/dev/null 2>&1; then
      removed=$((removed + 1))
    else
      say "warning: could not remove $ref"
    fi
  done <<<"$listing"
  say "removed $removed unused images, kept $kept"
}
