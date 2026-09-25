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

# Scans one image into a JSON report and never fails on its own; `report`
# below decides pass/fail from that file, so the scan and the
# failing-findings table read the same data instead of two divergent Trivy
# calls (one for the exit code, one for the printed table).
scan_image() {
  local trivy_bin=${1:?scan-image needs the trivy binary path}
  local ref=${2:?scan-image needs an image reference}
  local platform=${3:-}
  local report=${4:?scan-image needs a report output path}
  local -a args=(
    image --scanners vuln --ignore-unfixed
    --severity HIGH,CRITICAL --format json --output "$report"
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
  local has_results artifact_name count
  has_results=$(jq 'has("Results")' "$report")
  if [ "$has_results" != "true" ]; then
    printf 'security-scan: %s has no Results array; the image was never scanned\n' \
      "$report" >&2
    return 1
  fi
  artifact_name=$(jq -r '.ArtifactName // ""' "$report")
  if [ -z "$artifact_name" ]; then
    printf 'security-scan: %s has an empty ArtifactName; the image was never scanned\n' \
      "$report" >&2
    return 1
  fi
  count=$(jq '[.Results[]?.Vulnerabilities[]?] | length' "$report")
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

# The most recent released version tag, for the weekly scan's image targets.
# Needs full tag history (a shallow or single-branch checkout will not do).
latest_release_tag() {
  git describe --tags --abbrev=0 --match 'v*'
}

# The weekly scan's target list: the three released images at the latest
# tag, each into its own report under $2. Base images are not scanned on their
# own: the runtime stages remove packages the bases ship (the npm CLI), so a
# base finding may not reach production, and the released images already show
# every fixable finding that does. Continues past a failing target so one run
# shows the complete picture, then exits non-zero if any target had a fixable
# HIGH/CRITICAL finding or a scan error. summary.txt and failed.txt under the
# report directory are the job summary's source.
weekly_scan() {
  local trivy_bin=${1:?weekly-scan needs the trivy binary path}
  local report_dir=${2:?weekly-scan needs a report directory}
  mkdir -p "$report_dir"
  : >"$report_dir/summary.txt"
  local fail=0 tag name ref report

  tag=$(latest_release_tag)
  printf 'security-scan: scanning released images at %s\n' "$tag"
  for name in server web caddy; do
    ref="ghcr.io/dannyota/aboutme-${name}:${tag}"
    report="$report_dir/released-${name}.json"
    scan_image "$trivy_bin" "$ref" linux/arm64 "$report" || fail=1
    printf 'security-scan: result for %s\n' "$ref" | tee -a "$report_dir/summary.txt"
    if ! report_findings "$report" >>"$report_dir/summary.txt"; then
      printf '%s\n' "$ref" >>"$report_dir/failed.txt"
      fail=1
    fi
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
latest-tag)
  shift
  latest_release_tag "$@"
  ;;
weekly-scan)
  shift
  weekly_scan "$@"
  ;;
*)
  die "usage: $0 {install-trivy|scan-image|report|latest-tag|weekly-scan} [args...]"
  ;;
esac
