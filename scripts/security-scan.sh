#!/usr/bin/env bash

# Shared Trivy logic for the release-image scan (release-images.yml) and the
# weekly security scan (security-scan.yml): installing the pinned binary,
# scanning one image into a JSON report, and printing a compact table of any
# fixable HIGH/CRITICAL findings. One script keeps both workflow files from
# repeating the same install-and-scan steps.
set -Eeuo pipefail

TRIVY_VERSION=0.74.0
# sha256 values copied from trivy_0.74.0_checksums.txt, the release's own
# manifest at https://github.com/aquasecurity/trivy/releases/tag/v0.74.0.
# Both tarballs carry a GitHub build attestation, confirmed at pin time with
# `gh api repos/aquasecurity/trivy/attestations/sha256:<digest>`.
TRIVY_SHA256_LINUX_ARM64=b94ce1976bbf3c15b514b605ee88be7c6d94a29be2302847ff01cb794d47aad5
TRIVY_SHA256_LINUX_AMD64=2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a

die() {
  printf 'security-scan: %s\n' "$*" >&2
  exit 1
}

trivy_asset() {
  case "$(uname -m)" in
  aarch64 | arm64)
    printf '%s %s\n' "trivy_${TRIVY_VERSION}_Linux-ARM64.tar.gz" "$TRIVY_SHA256_LINUX_ARM64"
    ;;
  x86_64)
    printf '%s %s\n' "trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" "$TRIVY_SHA256_LINUX_AMD64"
    ;;
  *)
    die "unsupported architecture $(uname -m)"
    ;;
  esac
}

# Downloads the pinned Trivy release tarball for the current architecture
# straight from GitHub (never a third-party action), verifies it against the
# sha256 pinned above, and extracts the binary into $1.
install_trivy() {
  local dest=${1:?install-trivy needs a destination directory}
  local asset sha256 tmp
  read -r asset sha256 < <(trivy_asset)
  tmp=$(mktemp -d)
  trap 'rm -rf -- "$tmp"' RETURN
  curl -fsSL -o "$tmp/$asset" \
    "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/${asset}"
  echo "$sha256  $tmp/$asset" | sha256sum -c -
  mkdir -p "$dest"
  tar -xzf "$tmp/$asset" -C "$dest" trivy
  "$dest/trivy" --version
}

# Scans one image into a JSON report. Engine errors fail here; `report`
# below decides pass/fail from findings in that file, so the scan and the
# failing-findings table read the same data instead of two divergent Trivy
# calls (one for the exit code, one for the printed table).
scan_image() {
  local trivy_bin=${1:?scan-image needs the trivy binary path}
  local ref=${2:?scan-image needs an image reference}
  local platform=${3:-}
  local report=${4:?scan-image needs a report output path}
  local -a args=(
    image --scanners vuln --ignore-unfixed
    --severity 'HIGH,CRITICAL' --format json --output "$report"
  )
  [ -n "$platform" ] && args+=(--platform "$platform")
  args+=("$ref")
  "$trivy_bin" "${args[@]}"
}

# Prints a compact table (ID, package, installed, fixed) of the report's
# findings and exits non-zero when there are any. The report is already
# filtered to fixable HIGH/CRITICAL vulnerabilities, so every row here is a
# failing one; this is what keeps a public CI log from growing a full
# vulnerability dump, since the JSON itself goes to a short-retention
# artifact instead.
report_findings() {
  local report=${1:?report needs a report path}
  local count
  if ! jq -e '
    (.ArtifactName | type == "string" and length > 0) and
    (.Results | type == "array") and
    all(.Results[]; type == "object" and
      (.Vulnerabilities == null or (.Vulnerabilities | type == "array")))
  ' "$report" >/dev/null; then
    printf 'security-scan: %s is not a valid image scan report\n' \
      "$report" >&2
    return 1
  fi
  count=$(jq '[.Results[]?.Vulnerabilities[]?] | length' "$report") || return 1
  if [ "$count" -eq 0 ]; then
    printf 'security-scan: no fixable HIGH/CRITICAL findings in %s\n' "$report"
    return 0
  fi
  printf '%s\t%s\t%s\t%s\n' ID PACKAGE INSTALLED FIXED
  jq -r '
    .Results[]? .Vulnerabilities[]?
    | [.VulnerabilityID, .PkgName, .InstalledVersion, .FixedVersion] | @tsv
  ' "$report"
  return 1
}

# Resolve GitHub's latest published release, or an explicit version, and fetch
# its digest asset by ID. A reachable Git tag does not prove publication.
release_assets() {
  local requested=$1 dest=$2 endpoint=latest metadata selection tag asset_id
  mkdir -p "$dest" || return 1
  rm -f "$dest/tag.txt" "$dest/digests.txt" || return 1
  if [ -n "$requested" ]; then
    [[ $requested =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
      printf 'security-scan: expected a vMAJOR.MINOR.PATCH release\n' >&2
      return 1
    }
    endpoint="tags/$requested"
  fi
  metadata=$(gh api "repos/dannyota/aboutme/releases/$endpoint") || return 1
  selection=$(jq -er --arg requested "$requested" '
    select(.draft == false and .prerelease == false)
    | select(.published_at | type == "string" and length > 0)
    | select(.tag_name | test("^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$"))
    | select($requested == "" or .tag_name == $requested)
    | [.tag_name, ([.assets[] | select(.name == "digests.txt")]
        | select(length == 1) | .[0] | select(.state == "uploaded")
        | .id | select(type == "number" and . > 0 and . == floor))]
    | select(length == 2) | @tsv
  ' <<<"$metadata") || {
    printf 'security-scan: release lacks published version metadata or a unique digest asset\n' >&2
    return 1
  }
  read -r tag asset_id <<<"$selection"
  [[ $asset_id =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "$tag" >"$dest/tag.txt" || return 1
  gh api "repos/dannyota/aboutme/releases/assets/$asset_id" \
    -H Accept:application/octet-stream >"$dest/digests.txt" || return 1
  [ -s "$dest/digests.txt" ]
}

# Require exactly one platform manifest, never the app's multi-platform index.
release_image_ref() {
  local manifest=$1 name=$2 arch=$3 line ref platform extra
  line=$(awk -v image="ghcr.io/dannyota/aboutme-$name@" -v platform="linux/$arch" '
    index($1, image) == 1 && $2 == platform { print }
  ' "$manifest") || return 1
  [ -n "$line" ] && [[ $line != *$'\n'* ]] || return 1
  read -r ref platform extra <<<"$line"
  [ -z "$extra" ] && [ "$platform" = "linux/$arch" ] || return 1
  [[ $ref =~ ^ghcr\.io/dannyota/aboutme-$name@sha256:[0-9a-f]{64}$ ]] || return 1
  printf '%s\n' "$ref"
}

# Scan published platform digests. The observer may be retained from a separate
# release. Missing assets, findings, and engine errors fail the run without
# preventing collection of other targets. This is not deployed-version inventory.
weekly_scan() {
  local trivy_bin=${1:?weekly-scan needs the trivy binary path}
  local report_dir=${2:?weekly-scan needs a report directory}
  local requested=${3:-} observer_requested=${4:-}
  local fail=0 app_ready=0 observer_ready=0 name arch ref report source ready
  mkdir -p "$report_dir"
  : >"$report_dir/summary.txt"
  : >"$report_dir/failed.txt"
  rm -f "$report_dir/observer/tag.txt" "$report_dir/observer/digests.txt"

  if release_assets "$requested" "$report_dir/app"; then
    app_ready=1
  else
    printf 'App release resolution failed\n' | tee -a "$report_dir/summary.txt" >>"$report_dir/failed.txt"
    fail=1
  fi
  local observer_source="$report_dir/app"
  observer_ready=$app_ready
  if [ -n "$observer_requested" ]; then
    observer_source="$report_dir/observer"
    observer_ready=0
    if release_assets "$observer_requested" "$observer_source"; then
      observer_ready=1
    else
      printf 'Observer release resolution failed\n' | tee -a "$report_dir/summary.txt" >>"$report_dir/failed.txt"
      fail=1
    fi
  fi
  if [ -f "$report_dir/app/tag.txt" ]; then
    printf 'App release: %s\n' "$(cat "$report_dir/app/tag.txt")" >>"$report_dir/summary.txt"
  fi
  if [ -f "$observer_source/tag.txt" ]; then
    printf 'Observer release: %s\n' "$(cat "$observer_source/tag.txt")" >>"$report_dir/summary.txt"
  fi

  for name in server web caddy observer; do
    source="$report_dir/app"
    ready=$app_ready
    if [ "$name" = observer ]; then
      source=$observer_source
      ready=$observer_ready
    fi
    for arch in arm64 amd64; do
      report="$report_dir/released-${name}-${arch}.json"
      rm -f "$report"
      if [ "$ready" -ne 1 ] || ! ref=$(release_image_ref "$source/digests.txt" "$name" "$arch"); then
        printf '%s linux/%s: missing or invalid release digest\n' "$name" "$arch" |
          tee -a "$report_dir/summary.txt" >>"$report_dir/failed.txt"
        fail=1
        continue
      fi
      printf 'Scanning %s linux/%s\n' "$ref" "$arch" >>"$report_dir/summary.txt"
      if ! scan_image "$trivy_bin" "$ref" "linux/$arch" "$report"; then
        printf '%s linux/%s: scan engine error\n' "$ref" "$arch" |
          tee -a "$report_dir/summary.txt" >>"$report_dir/failed.txt"
        fail=1
      elif ! report_findings "$report" >>"$report_dir/summary.txt" 2>&1; then
        printf '%s linux/%s: findings or invalid report\n' "$ref" "$arch" >>"$report_dir/failed.txt"
        fail=1
      fi
    done
  done

  cat "$report_dir/summary.txt"
  return "$fail"
}

case "${1:-}" in
install-trivy)
  shift
  install_trivy "$@"
  ;;
scan-image)
  shift
  scan_image "$@"
  ;;
report)
  shift
  report_findings "$@"
  ;;
weekly-scan)
  shift
  weekly_scan "$@"
  ;;
*)
  die "usage: $0 {install-trivy|scan-image|report|weekly-scan} [args...]"
  ;;
esac
